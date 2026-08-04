package archive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

func chatCompletionServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(map[string]any{
			"id":      "gen-1",
			"model":   "test",
			"object":  "chat.completion",
			"choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}}},
		})
		_, _ = w.Write(body)
	}))
}

func TestAffinityScorer_NoOpWhenRickDidNotParticipate(t *testing.T) {
	srv := chatCompletionServer(t, `{"users":[{"user_id":"1","delta":5,"reason":"nice"}]}`)
	defer srv.Close()

	scorer := NewAffinityScorer(AffinityScorerConfig{Model: "test"}, llm.NewClientWithServerURL("test-key", srv.URL), &Affinity{})
	chunk := Chunk{Messages: []store.Message{
		{AuthorID: 1, IsBot: false},
		{AuthorID: 2, IsBot: false},
	}}

	if err := scorer.ScoreChunk(context.Background(), chunk, 999); err != nil {
		t.Fatalf("ScoreChunk returned error: %v", err)
	}
}

func TestAffinityScorer_NoOpWhenNoOtherParticipants(t *testing.T) {
	srv := chatCompletionServer(t, `{"users":[]}`)
	defer srv.Close()

	scorer := NewAffinityScorer(AffinityScorerConfig{Model: "test"}, llm.NewClientWithServerURL("test-key", srv.URL), &Affinity{})
	rickID := uint64(999)
	chunk := Chunk{Messages: []store.Message{
		{AuthorID: rickID, IsBot: true},
	}}

	if err := scorer.ScoreChunk(context.Background(), chunk, rickID); err != nil {
		t.Fatalf("ScoreChunk returned error: %v", err)
	}
}
