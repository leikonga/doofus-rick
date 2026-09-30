package ambient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/pgtest"
)

func chatCompletionServer(t *testing.T, content any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(map[string]any{
			"id":      "gen-1",
			"model":   "test",
			"object":  "chat.completion",
			"choices": []map[string]any{{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}}},
		})
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClassifyNullContentReturnsError(t *testing.T) {
	srv := chatCompletionServer(t, nil)
	c := NewClassifier(ClassifierConfig{Model: "test"}, llm.NewClientWithServerURL("test-key", srv.URL), pgtest.Store(t))

	messages := []llm.Message{llm.NewUserMessage(llm.TextPart("hi"))}
	if _, err := c.Classify(context.Background(), 1, messages); err == nil {
		t.Fatal("expected error for null completion content")
	}
}
