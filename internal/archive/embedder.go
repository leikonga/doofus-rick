package archive

import (
	"context"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

type EmbeddingConfig struct {
	Model string
	Dim   int
}

type Embedder struct {
	config EmbeddingConfig
	store  *store.Store
	llm    *llm.Client
}

func NewEmbedder(config EmbeddingConfig, s *store.Store, c *llm.Client) *Embedder {
	return &Embedder{config: config, store: s, llm: c}
}

func (e *Embedder) EmbedChunk(ctx context.Context, chunk store.Chunk) error {
	client := llm.NewClient("")

	req := llm.CompletionRequest{
		Model: e.config.Model,
	}

	resp, err := client.Embed(ctx, req)
	if err != nil {
		return err
	}

	if len(resp.Embeddings) == 0 {
		return nil
	}

	embedding := resp.Embeddings[0]

	truncated := truncateTo1024(embedding)

	storeEmbedding := store.ChunkEmbedding{
		ChunkID:   chunk.ID,
		Model:     e.config.Model,
		Embedding: truncated,
	}

	return e.store.SaveChunkEmbedding(ctx, storeEmbedding)
}

func truncateTo1024(vec []float32) []float32 {
	if len(vec) <= 1024 {
		return vec
	}

	result := make([]float32, 1024)
	copy(result, vec[:1024])

	sumSq := 0.0
	for _, v := range result {
		sumSq += float64(v) * float64(v)
	}
	if sumSq > 0 {
		norm := 1.0 / (sumSq + 1e-10)
		for i := range result {
			result[i] = float32(float64(result[i]) * norm)
		}
	}

	return result
}

func (e *Embedder) EmbedChunks(ctx context.Context, chunks []store.Chunk) error {
	for _, chunk := range chunks {
		if err := e.EmbedChunk(ctx, chunk); err != nil {
			return err
		}
	}
	return nil
}
