package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/disgoorg/disgo/events"
	"github.com/leikonga/doofus-rick/internal/codeedit"
)

func TestBuildToolsCountAndNames(t *testing.T) {
	a := &Agent{}
	event := &events.MessageCreate{GenericMessage: &events.GenericMessage{}}
	tools := a.buildTools(event)

	const want = 16
	if len(tools) != want {
		t.Fatalf("buildTools() returned %d tools, want %d", len(tools), want)
	}

	wantNames := []string{
		"decline", "discord_react", "web_media", "web_search", "web_fetch",
		"sys_shell", "discord_send_message", "discord_create_poll", "discord_send_file", "memory_quote_save",
		"memory_quote_list", "discord_schedule_reminder", "sys_logs", "memory_search",
		"code_read", "code_edit",
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

func TestCodeEditRejectsUnknownCommand(t *testing.T) {
	root := t.TempDir()
	ed, err := codeedit.New(root)
	if err != nil {
		t.Fatalf("codeedit.New: %v", err)
	}
	a := &Agent{codeedit: ed}
	event := &events.MessageCreate{GenericMessage: &events.GenericMessage{}}
	tools := a.buildTools(event)

	tool, ok := tools.Find("code_edit")
	if !ok {
		t.Fatal("code_edit tool not found")
	}
	_, err = tool.Execute(context.Background(), json.RawMessage(`{"command":"delete","path":"f.txt"}`))
	if err == nil {
		t.Fatal("code_edit Execute() with unknown command: expected error, got nil")
	}
}

func TestCodeReadSurfacesJailViolation(t *testing.T) {
	root := t.TempDir()
	ed, err := codeedit.New(root)
	if err != nil {
		t.Fatalf("codeedit.New: %v", err)
	}
	a := &Agent{codeedit: ed}
	event := &events.MessageCreate{GenericMessage: &events.GenericMessage{}}
	tools := a.buildTools(event)

	tool, ok := tools.Find("code_read")
	if !ok {
		t.Fatal("code_read tool not found")
	}
	_, err = tool.Execute(context.Background(), json.RawMessage(`{"path":"../outside.txt"}`))
	if err == nil {
		t.Fatal("code_read Execute() on path outside jail: expected error, got nil")
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
