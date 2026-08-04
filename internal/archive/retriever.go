package archive

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/leikonga/doofus-rick/internal/store"
)

type RetrievalConfig struct {
	TopK     int
	MinScore float64
}

type Retriever struct {
	config RetrievalConfig
	store  *store.Store
}

func NewRetriever(config RetrievalConfig, s *store.Store) *Retriever {
	if config.TopK == 0 {
		config.TopK = 3
	}
	if config.MinScore == 0 {
		config.MinScore = 0.02
	}
	return &Retriever{config: config, store: s}
}

type RetrievedChunk struct {
	ID             uint64
	ChannelID      uint64
	Content        string
	Score          float64
	LastActive     time.Time
	ChannelVisible bool
}

func (r *Retriever) Retrieve(ctx context.Context, query string, channelIDs []uint64) ([]RetrievedChunk, error) {
	var results []RetrievedChunk

	queryText := fmt.Sprintf("Instruct: Given a question, retrieve relevant chat logs\nQuery: %s", query)

	var chunks []struct {
		ID             uint64  `gorm:"column:id"`
		ChannelID      uint64  `gorm:"column:channel_id"`
		Content        string  `gorm:"column:content"`
		Score          float64 `gorm:"column:score"`
		LastActive     time.Time
		ChannelVisible bool
	}

	querySQL := `
		with vec as (
			select c.id, row_number() over (order by e.embedding <=> $1) as rank
			from chunks c join chunk_embeddings e on e.chunk_id = c.id
			where e.model = $5 and c.channel_id = any($2)
			order by e.embedding <=> $1 limit 50
		),
		lex as (
			select c.id, row_number() over (order by ts_rank_cd(tsv, q) desc) as rank
			from chunks c, plainto_tsquery('simple', $3) q
			where tsv @@ q and c.channel_id = any($2)
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
		limit $4;
	`

	err := r.store.DB().WithContext(ctx).Raw(querySQL, queryText, channelIDs, queryText, r.config.TopK, "qwen/qwen3-embedding-8b").Scan(&chunks).Error
	if err != nil {
		return nil, err
	}

	for _, c := range chunks {
		if c.Score >= r.config.MinScore {
			results = append(results, RetrievedChunk{
				ID:             c.ID,
				ChannelID:      c.ChannelID,
				Content:        c.Content,
				Score:          c.Score,
				LastActive:     c.LastActive,
				ChannelVisible: true,
			})
		}
	}

	return results, nil
}

func (r *Retriever) BuildRecallBlock(chunks []RetrievedChunk) string {
	if len(chunks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<recall>\n")
	for _, c := range chunks {
		sb.WriteString(c.Content + "\n")
	}
	sb.WriteString("</recall>\n")
	return sb.String()
}
