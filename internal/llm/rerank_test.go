package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientRerank_SendsRequestAndMapsResults(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Errorf("path = %q, want /rerank", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "voyageai/rerank-2.5-lite",
			"results": [
				{"index": 2, "relevance_score": 0.91, "document": {"text": "c"}},
				{"index": 0, "relevance_score": 0.12, "document": {"text": "a"}}
			],
			"usage": {"total_tokens": 42}
		}`))
	}))
	defer srv.Close()

	c := NewClientWithServerURL("test-key", srv.URL)
	resp, err := c.Rerank(context.Background(), RerankRequest{
		Model:     "voyageai/rerank-2.5-lite",
		Query:     "q",
		Documents: []string{"a", "b", "c"},
		TopN:      2,
	})
	if err != nil {
		t.Fatalf("Rerank returned error: %v", err)
	}

	if gotBody["model"] != "voyageai/rerank-2.5-lite" || gotBody["query"] != "q" {
		t.Errorf("unexpected model/query: %v", gotBody)
	}
	docs, _ := gotBody["documents"].([]any)
	if len(docs) != 3 || docs[0] != "a" || docs[2] != "c" {
		t.Errorf("documents = %v, want [a b c]", gotBody["documents"])
	}
	if gotBody["top_n"] != float64(2) {
		t.Errorf("top_n = %v, want 2", gotBody["top_n"])
	}
	want := []RerankResult{{Index: 2, Score: 0.91}, {Index: 0, Score: 0.12}}
	if len(resp.Results) != len(want) {
		t.Fatalf("results = %v, want %v", resp.Results, want)
	}
	for i, w := range want {
		if resp.Results[i] != w {
			t.Errorf("result[%d] = %v, want %v", i, resp.Results[i], w)
		}
	}
	if resp.TotalTokens != 42 {
		t.Errorf("TotalTokens = %d, want 42", resp.TotalTokens)
	}
}

func TestClientRerank_OmitsTopNWhenZeroAndToleratesMissingUsage(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model": "m", "results": [{"index": 0, "relevance_score": 0.5, "document": {"text": "a"}}]}`))
	}))
	defer srv.Close()

	c := NewClientWithServerURL("test-key", srv.URL)
	resp, err := c.Rerank(context.Background(), RerankRequest{Model: "m", Query: "q", Documents: []string{"a"}})
	if err != nil {
		t.Fatalf("Rerank returned error: %v", err)
	}
	if _, ok := gotBody["top_n"]; ok {
		t.Errorf("top_n sent: %v", gotBody["top_n"])
	}
	if resp.TotalTokens != 0 || len(resp.Results) != 1 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestClientRerank_ServerErrorIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": {"code": 400, "message": "bad"}}`))
	}))
	defer srv.Close()

	c := NewClientWithServerURL("test-key", srv.URL)
	if _, err := c.Rerank(context.Background(), RerankRequest{Model: "m", Query: "q", Documents: []string{"a"}}); err == nil {
		t.Fatal("expected error")
	}
}
