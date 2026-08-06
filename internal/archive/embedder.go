package archive

import (
	"context"
	"math"
	"strconv"

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

// maxEmbedBatchSize caps how many chunk contents go into a single OpenRouter
// embeddings request, avoiding provider-side batch limits.
const maxEmbedBatchSize = 20

func (e *Embedder) embedBatch(ctx context.Context, batch []store.Chunk) error {
	inputs := make([]string, len(batch))
	for i, chunk := range batch {
		inputs[i] = chunk.Content
	}

	resp, err := e.llm.Embed(ctx, llm.EmbeddingRequest{
		Model: e.config.Model,
		Input: inputs,
	})
	if err != nil {
		return err
	}
	e.store.SaveTokenUsage(ctx, strconv.FormatUint(batch[0].ChannelID, 10), "embedder", e.config.Model, resp.InputTokens, 0)

	for i, embedding := range resp.Embeddings {
		if i >= len(batch) {
			break
		}
		truncated := truncateTo1024(embedding)
		storeEmbedding := store.ChunkEmbedding{
			ChunkID:   batch[i].ID,
			Model:     e.config.Model,
			Embedding: store.HalfVector(truncated),
		}
		if err := e.store.SaveChunkEmbedding(ctx, storeEmbedding); err != nil {
			return err
		}
	}

	return nil
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
		invNorm := 1.0 / math.Sqrt(sumSq)
		for i := range result {
			result[i] = float32(float64(result[i]) * invNorm)
		}
	}

	return result
}

func (e *Embedder) EmbedChunks(ctx context.Context, chunks []store.Chunk) error {
	for start := 0; start < len(chunks); start += maxEmbedBatchSize {
		end := min(start+maxEmbedBatchSize, len(chunks))
		if err := e.embedBatch(ctx, chunks[start:end]); err != nil {
			return err
		}
	}
	return nil
}
