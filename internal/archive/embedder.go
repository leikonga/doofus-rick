package archive

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

type EmbeddingConfig struct {
	Model string
	Dim   int
}

type embeddingClient interface {
	Embed(ctx context.Context, req llm.EmbeddingRequest) (llm.EmbeddingResponse, error)
}

type Embedder struct {
	config   EmbeddingConfig
	store    *store.Store
	llm      embeddingClient
	chunker  *Chunker
	channels ChannelNamer
}

func NewEmbedder(config EmbeddingConfig, s *store.Store, c embeddingClient, chunker *Chunker, channels ChannelNamer) *Embedder {
	return &Embedder{config: config, store: s, llm: c, chunker: chunker, channels: channels}
}

const maxEmbedBatchSize = 20

func (e *Embedder) embedBatch(ctx context.Context, batch []store.Chunk) error {
	inputs := make([]string, len(batch))
	for i, chunk := range batch {
		text, err := e.embedText(ctx, chunk)
		if err != nil {
			return err
		}
		inputs[i] = text
	}

	resp, err := e.llm.Embed(ctx, llm.EmbeddingRequest{
		Model: e.config.Model,
		Input: inputs,
	})
	if err != nil {
		return err
	}
	if err := e.store.SaveTokenUsage(ctx, store.TokenUsage{ChannelID: strconv.FormatUint(batch[0].ChannelID, 10), UserID: "embedder", ModelName: e.config.Model, InputTokens: resp.InputTokens}); err != nil {
		slog.Warn("failed to save token usage", "error", err)
	}

	if len(resp.Embeddings) != len(batch) {
		return fmt.Errorf("embedding count %d does not match batch size %d", len(resp.Embeddings), len(batch))
	}

	for i, embedding := range resp.Embeddings {
		truncated := truncateTo1024(embedding)
		storeEmbedding := store.ChunkEmbedding{
			ChunkID:   batch[i].ID,
			Model:     e.config.Model,
			Embedding: store.HalfVector(truncated),
			Version:   store.CurrentEmbedVersion,
		}
		if err := e.store.SaveChunkEmbedding(ctx, storeEmbedding); err != nil {
			return err
		}
	}

	return nil
}

func (e *Embedder) embedText(ctx context.Context, chunk store.Chunk) (string, error) {
	messages, err := e.store.GetChunkMessages(ctx, chunk)
	if err != nil {
		return "", err
	}
	var channelName string
	if e.channels != nil {
		channelName = e.channels.ChannelName(ctx, chunk.ChannelID)
	}
	return e.chunker.BuildEmbedText(chunk, messages, channelName), nil
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
