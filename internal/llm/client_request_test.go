package llm

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/OpenRouterTeam/go-sdk/models/components"
)

func TestBuildChatRequestMarksSystemAsCacheBreakpoint(t *testing.T) {
	data, err := json.Marshal(buildChatRequest(CompletionRequest{Model: "m", System: "persona"}))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		CacheControl *struct{ Type string } `json:"cache_control"`
		Messages     []struct {
			Role    string `json:"role"`
			Content []struct {
				Text         string                 `json:"text"`
				CacheControl *struct{ Type string } `json:"cache_control"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	if decoded.CacheControl == nil || decoded.CacheControl.Type != "ephemeral" {
		t.Errorf("top-level cache_control = %+v, want ephemeral", decoded.CacheControl)
	}
	sys := decoded.Messages[0]
	if sys.Role != "system" || len(sys.Content) != 1 || sys.Content[0].Text != "persona" {
		t.Fatalf("system message = %+v", sys)
	}
	if sys.Content[0].CacheControl == nil || sys.Content[0].CacheControl.Type != "ephemeral" {
		t.Errorf("system cache_control = %+v, want ephemeral", sys.Content[0].CacheControl)
	}
}

func TestBuildChatRequestReasoningEffort(t *testing.T) {
	if got := buildChatRequest(CompletionRequest{Model: "m"}).Reasoning; got != nil {
		t.Errorf("empty effort should omit reasoning, got %+v", got)
	}

	data, err := json.Marshal(buildChatRequest(CompletionRequest{Model: "m", ReasoningEffort: "medium"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"reasoning":{"effort":"medium"}`) {
		t.Errorf("request %s missing reasoning effort", data)
	}
}

func TestReasoningDetailsRoundTripUnchanged(t *testing.T) {
	raw := `[{"type":"reasoning.encrypted","data":"opaque-blob","id":"r1","format":"anthropic-claude-v1","index":0}]`
	var details []components.ReasoningDetailUnion
	if err := json.Unmarshal([]byte(raw), &details); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	msg := fromSDKAssistantMessage(components.ChatAssistantMessage{ReasoningDetails: details})
	if len(msg.ReasoningDetails) == 0 {
		t.Fatal("reasoning details dropped on the way in")
	}

	data, err := json.Marshal(toSDKMessage(msg))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"data":"opaque-blob"`) || !strings.Contains(string(data), `"id":"r1"`) {
		t.Errorf("reasoning details not sent back unchanged: %s", data)
	}
}

func TestBuildChatRequestResponseSchema(t *testing.T) {
	unset, err := json.Marshal(buildChatRequest(CompletionRequest{Model: "m"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unset), "response_format") {
		t.Errorf("unset schema should omit response_format: %s", unset)
	}

	schema := map[string]any{"type": "object", "required": []any{"a"}}
	data, err := json.Marshal(buildChatRequest(CompletionRequest{
		Model:          "m",
		ResponseSchema: &ResponseSchema{Name: "thing", Schema: schema},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ResponseFormat struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Name   string         `json:"name"`
				Strict bool           `json:"strict"`
				Schema map[string]any `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
		Provider struct {
			RequireParameters bool `json:"require_parameters"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	rf := decoded.ResponseFormat
	if rf.Type != "json_schema" || rf.JSONSchema.Name != "thing" || !rf.JSONSchema.Strict {
		t.Errorf("response_format = %+v", rf)
	}
	if !reflect.DeepEqual(rf.JSONSchema.Schema, schema) {
		t.Errorf("schema = %v, want %v", rf.JSONSchema.Schema, schema)
	}
	if !decoded.Provider.RequireParameters {
		t.Errorf("require_parameters dropped: %s", data)
	}
}
