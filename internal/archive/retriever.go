package archive

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

const recallNote = "Possibly related old chat snippets. Often unrelated; ignore unless clearly relevant."

type RetrievalConfig struct {
	TopK           int
	MinSimilarity  float64
	EmbedModel     string
	NeighborChunks int
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

type RetrieveRequest struct {
	Query      string
	ChannelIDs []uint64
	AuthorID   *uint64
	Since      *time.Time
	Until      *time.Time
}

func (r *Retriever) Retrieve(ctx context.Context, req RetrieveRequest) ([]RetrievedChunk, error) {
	var results []RetrievedChunk
	query, channelIDs := req.Query, req.ChannelIDs

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
	if err := r.store.SaveTokenUsage(ctx, store.TokenUsage{ChannelID: channelKey, UserID: "retriever", ModelName: r.config.EmbedModel, InputTokens: embedResp.InputTokens}); err != nil {
		slog.Warn("failed to save token usage", "error", err)
	}
	if len(embedResp.Embeddings) == 0 {
		return nil, fmt.Errorf("archive: empty query embedding")
	}
	chunks, err := r.store.SearchChunks(ctx, store.ChunkSearch{
		Vector:     truncateTo1024(embedResp.Embeddings[0]),
		Query:      query,
		ChannelIDs: channelIDs,
		Model:      r.config.EmbedModel,
		TopK:       r.config.TopK,

		MinSimilarity: r.config.MinSimilarity,
		AuthorID:      req.AuthorID,
		Since:         req.Since,
		Until:         req.Until,
	})
	if err != nil {
		return nil, err
	}

	for _, c := range chunks {
		slog.Debug("archive candidate", "chunk_id", c.ID, "similarity", c.Similarity, "rrf", c.Score, "content", preview(c.Content, 80))
		content := c.Content
		if expanded, err := r.expandWithNeighbors(ctx, c.ChannelID, c.ID, content); err != nil {
			slog.Warn("failed to expand chunk with neighbors", "chunk_id", c.ID, "error", err)
		} else {
			content = expanded
		}
		results = append(results, RetrievedChunk{
			ID:             c.ID,
			ChannelID:      c.ChannelID,
			Content:        content,
			Score:          c.Score,
			LastActive:     c.LastActive,
			ChannelVisible: true,
		})
	}

	slog.Info("archive retrieval", "query", query, "candidates", len(chunks), "min_similarity", r.config.MinSimilarity,
		"author_id", formatOptional(req.AuthorID), "since", formatOptional(req.Since), "until", formatOptional(req.Until))

	return results, nil
}

func preview(content string, limit int) string {
	content = strings.Join(strings.Fields(content), " ")
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return string(runes[:limit])
}

func formatOptional[T any](p *T) string {
	if p == nil {
		return "none"
	}
	return fmt.Sprint(*p)
}

func (r *Retriever) expandWithNeighbors(ctx context.Context, channelID, chunkID uint64, content string) (string, error) {
	if r.config.NeighborChunks <= 0 {
		return content, nil
	}
	neighbors, err := r.store.GetNeighborChunks(ctx, channelID, chunkID, r.config.NeighborChunks, r.config.NeighborChunks)
	if err != nil {
		return "", err
	}

	parts := make([]string, 0, len(neighbors)+1)
	inserted := false
	for _, n := range neighbors {
		if !inserted && n.ID > chunkID {
			parts = append(parts, content)
			inserted = true
		}
		parts = append(parts, n.Content)
	}
	if !inserted {
		parts = append(parts, content)
	}
	return strings.Join(parts, "\n"), nil
}

func (r *Retriever) BuildRecallBlock(chunks []RetrievedChunk) string {
	if len(chunks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<recall>\n")
	sb.WriteString(recallNote)
	sb.WriteByte('\n')
	for _, c := range chunks {
		fmt.Fprintf(&sb, "<chunk date=%q>\n%s\n</chunk>\n", c.LastActive.Format("2006-01-02"), strings.TrimRight(c.Content, "\n"))
	}
	sb.WriteString("</recall>\n")
	return sb.String()
}
