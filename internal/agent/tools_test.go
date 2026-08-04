package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/disgoorg/disgo/events"
)

func TestBuildToolsCountAndNames(t *testing.T) {
	a := &Agent{}
	event := &events.MessageCreate{GenericMessage: &events.GenericMessage{}}
	tools := a.buildTools(event)

	const want = 14
	if len(tools) != want {
		t.Fatalf("buildTools() returned %d tools, want %d", len(tools), want)
	}

	wantNames := []string{
		"decline", "react", "media_search", "web_search", "fetch_page",
		"shell_exec", "send_message", "create_poll", "send_file", "save_quote",
		"get_user_quotes", "schedule_reminder", "check_logs", "search_history",
	}
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		seen[tool.Name] = true
	}
	for _, name := range wantNames {
		if !seen[name] {
			t.Errorf("buildTools() missing tool %q", name)
		}
	}
}

func TestBuildToolsDispatchRoutesToRightExecutor(t *testing.T) {
	a := &Agent{}
	event := &events.MessageCreate{GenericMessage: &events.GenericMessage{}}
	tools := a.buildTools(event)

	tool, ok := tools.Find("web_search")
	if !ok {
		t.Fatal("web_search tool not found")
	}
	if tool.Name != "web_search" {
		t.Errorf("Find(\"web_search\") returned tool named %q", tool.Name)
	}

	other, ok := tools.Find("decline")
	if !ok {
		t.Fatal("decline tool not found")
	}
	res, err := other.Execute(context.Background(), json.RawMessage(`{"emoji":"x"}`))
	if err != nil {
		t.Fatalf("decline Execute() error = %v", err)
	}
	if res.Response == nil || !res.Response.Decline || res.Response.Emoji != "x" {
		t.Errorf("decline Execute() result = %+v, want decline response with emoji x", res)
	}
}

func TestBuildToolsDispatchSurfacesUnmarshalError(t *testing.T) {
	a := &Agent{}
	event := &events.MessageCreate{GenericMessage: &events.GenericMessage{}}
	tools := a.buildTools(event)

	tool, ok := tools.Find("web_search")
	if !ok {
		t.Fatal("web_search tool not found")
	}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"query": 123}`))
	if err == nil {
		t.Fatal("expected unmarshal error for malformed input, got nil")
	}
}
