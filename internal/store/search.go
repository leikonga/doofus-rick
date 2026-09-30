package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ChunkSearch struct {
	Vector        []float32
	Query         string
	ChannelIDs    []uint64
	Model         string
	TopK          int
	MinSimilarity float64
	AuthorID      *uint64
	Since         *time.Time
	Until         *time.Time
}

type ScoredChunk struct {
	ID         uint64
	ChannelID  uint64
	Content    string
	Score      float64
	Similarity float64
	LastActive time.Time
}

const chunkFilterSQL = `
				and ((@author)::bigint is null or exists (
					select 1 from messages m
					where m.channel_id = c.channel_id and m.id between c.first_message_id and c.last_message_id
					  and m.author_id = (@author)::bigint))
				and ((@since)::timestamptz is null or c.ended_at >= (@since)::timestamptz)
				and ((@until)::timestamptz is null or c.started_at < (@until)::timestamptz)`

const hybridSearchSQL = `
			with vec as (
				select c.id, 1 - (e.embedding <=> (@vec)::halfvec) as sim,
				       row_number() over (order by e.embedding <=> (@vec)::halfvec) as rank
				from chunks c join chunk_embeddings e on e.chunk_id = c.id
				where e.model = @model and c.channel_id in (@channels)
				  and ((@minsim)::float8 <= 0 or 1 - (e.embedding <=> (@vec)::halfvec) >= (@minsim)::float8)` + chunkFilterSQL + `
				order by e.embedding <=> (@vec)::halfvec limit 50
			),
			lex as (
				select c.id, row_number() over (order by ts_rank_cd(tsv, q) desc) as rank
				from chunks c, plainto_tsquery('simple', @query) q
				where tsv @@ q and c.channel_id in (@channels)` + chunkFilterSQL + `
				order by ts_rank_cd(tsv, q) desc limit 50
			)
			select c.id, c.channel_id, c.content,
			       coalesce(1.0/(60 + vec.rank), 0) + coalesce(1.0/(60 + lex.rank), 0) as score,
			       coalesce(vec.sim, 0) as similarity,
			       c.ended_at as last_active
			from chunks c
			left join vec on vec.id = c.id
			left join lex on lex.id = c.id
			where vec.id is not null or lex.id is not null
			order by score desc
			limit @topk;
		`

func (s *Store) SearchChunks(ctx context.Context, q ChunkSearch) ([]ScoredChunk, error) {
	var rows []struct {
		ID         uint64  `gorm:"column:id"`
		ChannelID  uint64  `gorm:"column:channel_id"`
		Content    string  `gorm:"column:content"`
		Score      float64 `gorm:"column:score"`
		Similarity float64 `gorm:"column:similarity"`
		LastActive time.Time
	}
	err := s.db.WithContext(ctx).Raw(hybridSearchSQL,
		sql.Named("vec", vectorLiteral(q.Vector)),
		sql.Named("channels", q.ChannelIDs),
		sql.Named("query", q.Query),
		sql.Named("topk", q.TopK),
		sql.Named("model", q.Model),
		sql.Named("minsim", q.MinSimilarity),
		sql.Named("author", nullable(q.AuthorID)),
		sql.Named("since", nullable(q.Since)),
		sql.Named("until", nullable(q.Until)),
	).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("hybrid search chunks: %w", err)
	}
	chunks := make([]ScoredChunk, len(rows))
	for i, r := range rows {
		chunks[i] = ScoredChunk(r)
	}
	return chunks, nil
}

func nullable[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func vectorLiteral(vec []float32) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = strconv.FormatFloat(float64(v), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
