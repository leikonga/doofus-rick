package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientEmbed_SendsInputAndModel(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("path = %q, want /embeddings", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object": "list",
			"model": "qwen/qwen3-embedding-8b",
			"data": [{"object": "embedding", "index": 0, "embedding": [0.1, 0.2, 0.3]}],
			"usage": {"prompt_tokens": 7, "total_tokens": 7}
		}`))
	}))
	defer srv.Close()

	c := NewClientWithServerURL("test-key", srv.URL)
	resp, err := c.Embed(context.Background(), EmbeddingRequest{
		Model: "qwen/qwen3-embedding-8b",
		Input: []string{"hello world"},
	})
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}

	if gotBody["model"] != "qwen/qwen3-embedding-8b" {
		t.Errorf("request model = %v, want qwen/qwen3-embedding-8b", gotBody["model"])
	}
	input, ok := gotBody["input"].([]any)
	if !ok || len(input) != 1 || input[0] != "hello world" {
		t.Errorf("request input = %v, want [hello world]", gotBody["input"])
	}

	if len(resp.Embeddings) != 1 {
		t.Fatalf("got %d embeddings, want 1", len(resp.Embeddings))
	}
	want := []float32{0.1, 0.2, 0.3}
	for i, v := range want {
		if resp.Embeddings[0][i] != v {
			t.Errorf("embedding[%d] = %v, want %v", i, resp.Embeddings[0][i], v)
		}
	}
	if resp.InputTokens != 7 {
		t.Errorf("InputTokens = %d, want 7", resp.InputTokens)
	}
}

func TestClientEmbed_EmptyResponseIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object": "list", "model": "m", "data": []}`))
	}))
	defer srv.Close()

	c := NewClientWithServerURL("test-key", srv.URL)
	resp, err := c.Embed(context.Background(), EmbeddingRequest{Model: "m", Input: []string{"x"}})
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if len(resp.Embeddings) != 0 {
		t.Errorf("got %d embeddings, want 0", len(resp.Embeddings))
	}
}
