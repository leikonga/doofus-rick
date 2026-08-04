package llm

import (
	"encoding/json"
	"testing"
)

func TestToolResultMessagesOnePerCallWithMatchingID(t *testing.T) {
	calls := []ToolCall{
		{ID: "call_1", Name: "web_search", Arguments: `{"query":"a"}`},
		{ID: "call_2", Name: "react", Arguments: `{"emojis":["x"]}`},
	}
	results := []string{"result-a", "result-b"}

	var messages []Message
	for i, c := range calls {
		messages = append(messages, NewToolResultMessage(c.ID, results[i]))
	}

	if len(messages) != len(calls) {
		t.Fatalf("got %d messages, want %d (one per tool call)", len(messages), len(calls))
	}

	for i, m := range messages {
		if m.Role != RoleTool {
			t.Errorf("message %d role = %q, want %q", i, m.Role, RoleTool)
		}
		if m.ToolCallID != calls[i].ID {
			t.Errorf("message %d tool_call_id = %q, want %q", i, m.ToolCallID, calls[i].ID)
		}

		sdkMsg := toSDKMessage(m)
		data, err := json.Marshal(sdkMsg)
		if err != nil {
			t.Fatalf("marshal message %d: %v", i, err)
		}
		var decoded struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			Content    string `json:"content"`
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal message %d: %v", i, err)
		}
		if decoded.Role != "tool" {
			t.Errorf("message %d wire role = %q, want %q", i, decoded.Role, "tool")
		}
		if decoded.ToolCallID != calls[i].ID {
			t.Errorf("message %d wire tool_call_id = %q, want %q", i, decoded.ToolCallID, calls[i].ID)
		}
		if decoded.Content != results[i] {
			t.Errorf("message %d wire content = %q, want %q", i, decoded.Content, results[i])
		}
	}
}

func TestAssistantMessageCarriesToolCallsNotResults(t *testing.T) {
	assistant := Message{
		Role: RoleAssistant,
		ToolCalls: []ToolCall{
			{ID: "call_1", Name: "web_search", Arguments: `{"query":"a"}`},
		},
	}

	sdkMsg := toSDKMessage(assistant)
	data, err := json.Marshal(sdkMsg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Role      string `json:"role"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Role != "assistant" {
		t.Errorf("role = %q, want assistant", decoded.Role)
	}
	if len(decoded.ToolCalls) != 1 || decoded.ToolCalls[0].ID != "call_1" {
		t.Errorf("tool_calls = %+v, want one call with id call_1", decoded.ToolCalls)
	}
}
