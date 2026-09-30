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
	Vector     []float32
	Query      string
	ChannelIDs []uint64
	Model      string
	TopK       int
}

type ScoredChunk struct {
	ID         uint64
	ChannelID  uint64
	Content    string
	Score      float64
	LastActive time.Time
}

const hybridSearchSQL = `
		with vec as (
			select c.id, row_number() over (order by e.embedding <=> (@vec)::halfvec) as rank
			from chunks c join chunk_embeddings e on e.chunk_id = c.id
			where e.model = @model and c.channel_id in (@channels)
			order by e.embedding <=> (@vec)::halfvec limit 50
		),
		lex as (
			select c.id, row_number() over (order by ts_rank_cd(tsv, q) desc) as rank
			from chunks c, plainto_tsquery('simple', @query) q
			where tsv @@ q and c.channel_id in (@channels)
			order by ts_rank_cd(tsv, q) desc limit 50
		)
		select c.id, c.channel_id, c.content,
		       coalesce(1.0/(60 + vec.rank), 0) + coalesce(1.0/(60 + lex.rank), 0) as score,
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
		LastActive time.Time
	}
	err := s.db.WithContext(ctx).Raw(hybridSearchSQL,
		sql.Named("vec", vectorLiteral(q.Vector)),
		sql.Named("channels", q.ChannelIDs),
		sql.Named("query", q.Query),
		sql.Named("topk", q.TopK),
		sql.Named("model", q.Model),
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

func vectorLiteral(vec []float32) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = strconv.FormatFloat(float64(v), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
