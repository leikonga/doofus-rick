package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/leikonga/doofus-rick/internal/llm"
)

type recordedTool struct {
	name   string
	result string
	isErr  bool
}

type fakeRecorder struct {
	tools []recordedTool
}

func (r *fakeRecorder) AddTool(name, _, result string, isErr bool) {
	r.tools = append(r.tools, recordedTool{name, result, isErr})
}

type startLog struct {
	mu    sync.Mutex
	names []string
}

func (l *startLog) add(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names = append(l.names, name)
}

func sleepTool(name string, d time.Duration, result llm.Result) llm.Tool {
	return llm.Tool{
		Name: name,
		Execute: func(context.Context, json.RawMessage) (llm.Result, error) {
			time.Sleep(d)
			return result, nil
		},
	}
}

func calls(names ...string) []llm.ToolCall {
	out := make([]llm.ToolCall, len(names))
	for i, n := range names {
		out[i] = llm.ToolCall{ID: "id-" + n, Name: n, Arguments: "{}"}
	}
	return out
}

func silenceLogs(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

func TestRunToolCalls_Timing(t *testing.T) {
	tests := []struct {
		name      string
		tools     llm.Tools
		calls     []llm.ToolCall
		wantTime  time.Duration
		wantStart []string
	}{
		{
			name:     "three plain calls run concurrently",
			tools:    llm.Tools{sleepTool("a", time.Second, llm.Result{Content: "a"}), sleepTool("b", time.Second, llm.Result{Content: "b"}), sleepTool("c", time.Second, llm.Result{Content: "c"})},
			calls:    calls("a", "b", "c"),
			wantTime: time.Second,
		},
		{
			name:      "code calls serialize in call order",
			tools:     llm.Tools{sleepTool("code_edit", time.Second, llm.Result{Content: "e"}), sleepTool("code_ship", time.Second, llm.Result{Content: "s"})},
			calls:     calls("code_ship", "code_edit"),
			wantTime:  2 * time.Second,
			wantStart: []string{"code_ship", "code_edit"},
		},
		{
			name:      "plain calls run alongside code lane",
			tools:     llm.Tools{sleepTool("code_edit", time.Second, llm.Result{Content: "e"}), sleepTool("code_ship", time.Second, llm.Result{Content: "s"}), sleepTool("web", 2*time.Second, llm.Result{Content: "w"})},
			calls:     calls("code_edit", "web", "code_ship"),
			wantTime:  2 * time.Second,
			wantStart: []string{"code_edit", "code_ship"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				starts := &startLog{}
				tools := make(llm.Tools, len(tt.tools))
				for i, tool := range tt.tools {
					execute := tool.Execute
					tools[i] = tool
					tools[i].Execute = func(ctx context.Context, in json.RawMessage) (llm.Result, error) {
						if strings.HasPrefix(tool.Name, "code_") {
							starts.add(tool.Name)
						}
						return execute(ctx, in)
					}
				}

				begin := time.Now()
				outcomes := runToolCalls(t.Context(), tools, tt.calls)
				if got := time.Since(begin); got != tt.wantTime {
					t.Fatalf("elapsed = %v, want %v", got, tt.wantTime)
				}
				if !slices.Equal(starts.names, tt.wantStart) {
					t.Fatalf("code starts = %v, want %v", starts.names, tt.wantStart)
				}
				for i, call := range tt.calls {
					want := map[string]string{"a": "a", "b": "b", "c": "c", "code_edit": "e", "code_ship": "s", "web": "w"}[call.Name]
					if outcomes[i].result.Content != want {
						t.Fatalf("outcome %d = %q, want %q", i, outcomes[i].result.Content, want)
					}
				}
			})
		})
	}
}

func TestRunToolCalls_PanicBecomesError(t *testing.T) {
	silenceLogs(t)
	synctest.Test(t, func(t *testing.T) {
		tools := llm.Tools{
			{Name: "boom", Execute: func(context.Context, json.RawMessage) (llm.Result, error) { panic("kaboom") }},
			sleepTool("ok", time.Second, llm.Result{Content: "fine"}),
			{Name: "code_boom", Execute: func(context.Context, json.RawMessage) (llm.Result, error) { panic("lane") }},
			sleepTool("code_ok", time.Second, llm.Result{Content: "lane fine"}),
		}
		outcomes := runToolCalls(t.Context(), tools, calls("boom", "ok", "code_boom", "code_ok"))

		if err := outcomes[0].err; err == nil || !strings.Contains(err.Error(), "kaboom") {
			t.Fatalf("panic outcome err = %v", err)
		}
		if err := outcomes[2].err; err == nil || !strings.Contains(err.Error(), "lane") {
			t.Fatalf("code lane panic outcome err = %v", err)
		}
		if outcomes[1].result.Content != "fine" || outcomes[3].result.Content != "lane fine" {
			t.Fatalf("other calls did not complete: %+v", outcomes)
		}
	})
}

func TestRunToolCalls_PassesTurnContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	tools := llm.Tools{{Name: "ctx", Execute: func(ctx context.Context, _ json.RawMessage) (llm.Result, error) {
		return llm.Result{}, ctx.Err()
	}}}
	outcomes := runToolCalls(ctx, tools, calls("ctx"))
	if !errors.Is(outcomes[0].err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", outcomes[0].err)
	}
}

func TestToolBatch(t *testing.T) {
	silenceLogs(t)
	first := &llm.RickResponse{Text: "first"}
	second := &llm.RickResponse{Text: "second"}
	tools := llm.Tools{
		sleepTool("slow_terminal", 2*time.Second, llm.Result{Response: first}),
		sleepTool("fast_terminal", 0, llm.Result{Response: second}),
		sleepTool("done", time.Second, llm.Result{Content: "bye", Done: true}),
		sleepTool("plain", time.Second, llm.Result{Content: "out"}),
		{Name: "fails", Execute: func(context.Context, json.RawMessage) (llm.Result, error) { return llm.Result{}, errors.New("nope") }},
	}
	tests := []struct {
		name         string
		calls        []llm.ToolCall
		wantTerminal *llm.RickResponse
		wantDone     bool
		wantMessages []string
		wantRecorded []recordedTool
	}{
		{
			name:         "first terminal in call order wins",
			calls:        calls("plain", "slow_terminal", "fast_terminal"),
			wantTerminal: first,
			wantRecorded: []recordedTool{{"plain", "out", false}, {"slow_terminal", "(terminal)", false}},
		},
		{
			name:         "done still records later calls",
			calls:        calls("done", "plain"),
			wantDone:     true,
			wantMessages: []string{"bye", "out"},
			wantRecorded: []recordedTool{{"done", "bye", false}, {"plain", "out", false}},
		},
		{
			name:         "unknown tool and error keep call order",
			calls:        calls("fails", "missing", "plain"),
			wantMessages: []string{"nope", `error: no tool named "missing"; available tools: slow_terminal, fast_terminal, done, plain, fails`, "out"},
			wantRecorded: []recordedTool{
				{"fails", "nope", true},
				{"missing", `error: no tool named "missing"; available tools: slow_terminal, fast_terminal, done, plain, fails`, true},
				{"plain", "out", false},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rec := &fakeRecorder{}
				outcomes := runToolCalls(t.Context(), tools, tt.calls)
				terminal, msgs, done := collectToolResults(rec, tools, tt.calls, outcomes)

				if terminal != tt.wantTerminal {
					t.Fatalf("terminal = %+v, want %+v", terminal, tt.wantTerminal)
				}
				if done != tt.wantDone {
					t.Fatalf("done = %v, want %v", done, tt.wantDone)
				}
				var gotMessages []string
				for _, m := range msgs {
					gotMessages = append(gotMessages, m.Text())
				}
				if !slices.Equal(gotMessages, tt.wantMessages) {
					t.Fatalf("messages = %q, want %q", gotMessages, tt.wantMessages)
				}
				if !slices.Equal(rec.tools, tt.wantRecorded) {
					t.Fatalf("recorded = %+v, want %+v", rec.tools, tt.wantRecorded)
				}
			})
		})
	}
}
