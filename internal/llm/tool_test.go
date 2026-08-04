package llm

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type sampleIn struct {
	Query     string `json:"query" jsonschema:"required,description=Search query."`
	Freshness string `json:"freshness" jsonschema:"description=pd=24h pw=7d pm=31d py=1y"`
}

type noIn struct{}

func TestNewToolSchema(t *testing.T) {
	tool := NewTool("web_search", "Search the web.", func(_ context.Context, in sampleIn) (Result, error) {
		return Result{Content: in.Query}, nil
	})

	if tool.Schema["type"] != "object" {
		t.Fatalf("schema type = %v, want object", tool.Schema["type"])
	}

	props, ok := tool.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing or wrong type: %v", tool.Schema["properties"])
	}
	if _, ok := props["query"]; !ok {
		t.Fatal("schema missing query property")
	}
	if _, ok := props["freshness"]; !ok {
		t.Fatal("schema missing freshness property")
	}

	required, ok := tool.Schema["required"].([]any)
	if !ok {
		t.Fatalf("schema required missing or wrong type: %v", tool.Schema["required"])
	}
	if len(required) != 1 || required[0] != "query" {
		t.Errorf("required = %v, want [query]", required)
	}
}

func TestNewToolExecuteUnmarshalsInput(t *testing.T) {
	tool := NewTool("web_search", "Search the web.", func(_ context.Context, in sampleIn) (Result, error) {
		return Result{Content: in.Query + "|" + in.Freshness}, nil
	})

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"cats","freshness":"pd"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if res.Content != "cats|pd" {
		t.Errorf("Content = %q, want %q", res.Content, "cats|pd")
	}
}

func TestNewToolExecuteSurfacesUnmarshalError(t *testing.T) {
	tool := NewTool("web_search", "Search the web.", func(_ context.Context, in sampleIn) (Result, error) {
		return Result{Content: in.Query}, nil
	})

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"query": 123}`))
	if err == nil {
		t.Fatal("expected unmarshal error, got nil")
	}
}

func TestToolsFindDispatchesToRightExecutor(t *testing.T) {
	var called string
	tools := Tools{
		NewTool("a", "", func(_ context.Context, _ noIn) (Result, error) {
			called = "a"
			return Result{Content: "a-result"}, nil
		}),
		NewTool("b", "", func(_ context.Context, _ noIn) (Result, error) {
			called = "b"
			return Result{Content: "b-result"}, nil
		}),
	}

	tool, ok := tools.Find("b")
	if !ok {
		t.Fatal("Find(\"b\") not found")
	}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if called != "b" {
		t.Errorf("called = %q, want %q", called, "b")
	}
	if res.Content != "b-result" {
		t.Errorf("Content = %q, want %q", res.Content, "b-result")
	}

	if _, ok := tools.Find("missing"); ok {
		t.Error("Find(\"missing\") should not be found")
	}
}

func TestToolExecuteErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	tool := NewTool("fails", "", func(_ context.Context, _ noIn) (Result, error) {
		return Result{}, wantErr
	})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}
