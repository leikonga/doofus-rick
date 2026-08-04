package archive

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

type RetrievalConfig struct {
	TopK       int
	MinScore   float64
	EmbedModel string
}

type Retriever struct {
	config RetrievalConfig
	store  *store.Store
	llm    *llm.Client
}

func NewRetriever(config RetrievalConfig, s *store.Store, c *llm.Client) *Retriever {
	if config.TopK == 0 {
		config.TopK = 3
	}
	if config.MinScore == 0 {
		config.MinScore = 0.02
	}
	return &Retriever{config: config, store: s, llm: c}
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

	embedResp, err := r.llm.Embed(ctx, llm.EmbeddingRequest{
		Model: r.config.EmbedModel,
		Input: []string{queryText},
	})
	if err != nil {
		return nil, err
	}
	channelKey := "0"
	if len(channelIDs) > 0 {
		channelKey = strconv.FormatUint(channelIDs[0], 10)
	}
	r.store.SaveTokenUsage(ctx, channelKey, "retriever", r.config.EmbedModel, embedResp.InputTokens, 0)
	if len(embedResp.Embeddings) == 0 {
		return nil, fmt.Errorf("archive: empty query embedding")
	}
	queryVector := vectorLiteral(truncateTo1024(embedResp.Embeddings[0]))

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

	err = r.store.DB().WithContext(ctx).Raw(querySQL,
		sql.Named("vec", queryVector),
		sql.Named("channels", channelIDs),
		sql.Named("query", query),
		sql.Named("topk", r.config.TopK),
		sql.Named("model", r.config.EmbedModel),
	).Scan(&chunks).Error
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

// vectorLiteral renders a vector in pgvector's text input format, e.g.
// "[0.1,0.2,0.3]", for binding against a halfvec column in a raw query.
func vectorLiteral(vec []float32) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = strconv.FormatFloat(float64(v), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
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
