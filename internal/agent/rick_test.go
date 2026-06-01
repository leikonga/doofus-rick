package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

// --- mock DiscordState ---

type mockDiscord struct {
	users map[string]string
}

func (m *mockDiscord) GetMemberForID(_ string) (*discord.Member, error) { return nil, nil }
func (m *mockDiscord) GetUsernameForID(id string) (string, error) {
	if name, ok := m.users[id]; ok {
		return name, nil
	}
	return "", fmt.Errorf("user %s not found", id)
}
func (m *mockDiscord) OnlineMembers() []discord.Member                { return nil }
func (m *mockDiscord) AllMembers() ([]discord.Member, error)          { return nil, nil }
func (m *mockDiscord) VoiceChannels() map[snowflake.ID]string         { return nil }
func (m *mockDiscord) VoiceChannelForID(_ string) string              { return "" }
func (m *mockDiscord) GetStatusForID(_ string) discord.OnlineStatus   { return "" }
func (m *mockDiscord) GetActivitiesForID(_ string) []discord.Activity { return nil }

func newTestAgent(users map[string]string) *Agent {
	return &Agent{discord: &mockDiscord{users: users}}
}

// --- helpers ---

func textMsg() anthropic.MessageParam {
	return anthropic.NewUserMessage(anthropic.NewTextBlock("hello"))
}

func toolResultMsg(id string) anthropic.MessageParam {
	return anthropic.NewUserMessage(anthropic.NewToolResultBlock(id, "result", false))
}

func assistantMsg() anthropic.MessageParam {
	return anthropic.NewAssistantMessage(anthropic.NewTextBlock("response"))
}

func assistantWithToolUse(id string) anthropic.MessageParam {
	return anthropic.NewAssistantMessage(anthropic.NewToolUseBlock(id, json.RawMessage(`{}`), "some_tool"))
}

// --- Tier 1: buildPrompt ---

func TestBuildPrompt(t *testing.T) {
	tests := []struct {
		name        string
		channelName string
		topic       string
		lines       []string
		trigger     string
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:       "empty inputs",
			wantAbsent: []string{"channel:", "topic:", "context", "mention"},
		},
		{
			name:        "channel only",
			channelName: "general",
			wantContain: []string{"# channel: general"},
			wantAbsent:  []string{"topic"},
		},
		{
			name:        "channel and topic",
			channelName: "general",
			topic:       "off-topic stuff",
			wantContain: []string{"# channel: general", "# topic: off-topic stuff"},
		},
		{
			name:        "lines only",
			lines:       []string{"[12:00 alice]: hi", "[12:01 bob]: hey"},
			wantContain: []string{"[context - recent chat", "[12:00 alice]: hi", "[12:01 bob]: hey"},
		},
		{
			name:        "trigger only",
			trigger:     "[alice]: ping",
			wantContain: []string{"[reply to this mention]", "[alice]: ping"},
		},
		{
			name:        "all fields",
			channelName: "general",
			topic:       "chat",
			lines:       []string{"[12:00 alice]: hi"},
			trigger:     "[bob]: what up",
			wantContain: []string{"# channel: general", "# topic: chat", "[context - recent chat", "[reply to this mention]"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildPrompt(tc.channelName, tc.topic, tc.lines, tc.trigger)
			for _, want := range tc.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in output:\n%s", want, got)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("unexpected %q in output:\n%s", absent, got)
				}
			}
		})
	}
}

// --- Tier 1: resolveMentions ---

func TestResolveMentions(t *testing.T) {
	a := newTestAgent(map[string]string{
		"123": "alice",
		"456": "bob",
	})

	tests := []struct {
		input string
		want  string
	}{
		{"hello world", "hello world"},
		{"hey <@123>", "hey @alice"},
		{"hey <@!123>", "hey @alice"},
		{"<@123> and <@456>", "@alice and @bob"},
		{"<@999>", "@unknown-user"},
		{"no mentions here", "no mentions here"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := a.resolveMentions(tc.input)
			if got != tc.want {
				t.Errorf("resolveMentions(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// --- Tier 1: trailingTagRe ---

func TestTrailingTagRe(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello world", "hello world"},
		{"hello <thinking>", "hello"},
		{"hello </thinking>", "hello"},
		{"hi <tag1> <tag2>", "hi"},
		{"text <tag1>\n<tag2>  ", "text"},
		{"keep <middle> tag me", "keep <middle> tag me"},
		{"<only tag>", ""},
		{"text\n\n<thinking>  ", "text"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := strings.TrimSpace(trailingTagRe.ReplaceAllString(tc.input, ""))
			if got != tc.want {
				t.Errorf("trailingTagRe(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// --- Tier 1: isUserTurnStart ---

func TestIsUserTurnStart(t *testing.T) {
	tests := []struct {
		name string
		msg  anthropic.MessageParam
		want bool
	}{
		{"text user message", textMsg(), true},
		{"tool_result user message", toolResultMsg("id1"), false},
		{"assistant message", assistantMsg(), false},
		{"assistant with tool_use", assistantWithToolUse("id1"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isUserTurnStart(tc.msg)
			if got != tc.want {
				t.Errorf("isUserTurnStart() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- Tier 1: putSession trimming ---

func TestPutSession(t *testing.T) {
	a := &Agent{}
	id := snowflake.ID(1)

	tests := []struct {
		name     string
		messages []anthropic.MessageParam
		wantLen  int
		wantOK   bool // first message must pass isUserTurnStart
	}{
		{
			name:     "empty",
			messages: nil,
			wantLen:  0,
		},
		{
			name:     "single valid user message",
			messages: []anthropic.MessageParam{textMsg()},
			wantLen:  1,
			wantOK:   true,
		},
		{
			name:     "leading tool_result is dropped",
			messages: []anthropic.MessageParam{toolResultMsg("id1")},
			wantLen:  0,
		},
		{
			name:     "leading tool_result followed by valid message",
			messages: []anthropic.MessageParam{toolResultMsg("id1"), textMsg()},
			wantLen:  1,
			wantOK:   true,
		},
		{
			name:     "valid sequence preserved",
			messages: []anthropic.MessageParam{textMsg(), assistantMsg(), textMsg()},
			wantLen:  3,
			wantOK:   true,
		},
		{
			name: "trimmed to maxSessionMsgs, orphan dropped",
			messages: func() []anthropic.MessageParam {
				// maxSessionMsgs+1 messages; trim takes last maxSessionMsgs.
				// Put orphan at index 1 so it becomes index 0 after trim.
				msgs := make([]anthropic.MessageParam, maxSessionMsgs+1)
				for i := range msgs {
					msgs[i] = textMsg()
				}
				msgs[1] = toolResultMsg("orphan")
				return msgs
			}(),
			// trim leaves orphan at front; putSession drops it
			wantLen: maxSessionMsgs - 1,
			wantOK:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a.putSession(id, tc.messages)
			got := a.getSession(id)
			if len(got) != tc.wantLen {
				t.Errorf("session len = %d, want %d", len(got), tc.wantLen)
			}
			if tc.wantOK && len(got) > 0 && !isUserTurnStart(got[0]) {
				t.Error("first message fails isUserTurnStart after putSession")
			}
		})
	}
}

// --- Tier 2: validateConversation ---

func TestValidateConversation(t *testing.T) {
	tests := []struct {
		name        string
		messages    []anthropic.MessageParam
		wantLen     int
		wantRepair  bool
		wantValidFn func([]anthropic.MessageParam) bool
	}{
		{
			name:       "nil slice",
			messages:   nil,
			wantLen:    0,
			wantRepair: false,
		},
		{
			name:       "empty slice",
			messages:   []anthropic.MessageParam{},
			wantLen:    0,
			wantRepair: false,
		},
		{
			name:       "valid single user message",
			messages:   []anthropic.MessageParam{textMsg()},
			wantLen:    1,
			wantRepair: false,
		},
		{
			name:       "valid conversation",
			messages:   []anthropic.MessageParam{textMsg(), assistantMsg(), textMsg()},
			wantLen:    3,
			wantRepair: false,
		},
		{
			name:       "single orphan tool_result",
			messages:   []anthropic.MessageParam{toolResultMsg("id1")},
			wantLen:    0,
			wantRepair: true,
		},
		{
			name: "orphan tool_result followed by valid message",
			messages: []anthropic.MessageParam{
				toolResultMsg("id1"),
				textMsg(),
			},
			wantLen:    1,
			wantRepair: true,
			wantValidFn: func(msgs []anthropic.MessageParam) bool {
				return isUserTurnStart(msgs[0])
			},
		},
		{
			name: "multiple leading orphans dropped",
			messages: []anthropic.MessageParam{
				toolResultMsg("id1"),
				toolResultMsg("id2"),
				textMsg(),
			},
			wantLen:    1,
			wantRepair: true,
		},
		{
			name: "assistant message at front is orphan",
			messages: []anthropic.MessageParam{
				assistantMsg(),
				textMsg(),
			},
			wantLen:    1,
			wantRepair: true,
			wantValidFn: func(msgs []anthropic.MessageParam) bool {
				return isUserTurnStart(msgs[0])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, repaired := validateConversation(tc.messages)
			if repaired != tc.wantRepair {
				t.Errorf("repaired = %v, want %v", repaired, tc.wantRepair)
			}
			if len(got) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(got), tc.wantLen)
			}
			if tc.wantValidFn != nil && !tc.wantValidFn(got) {
				t.Error("result failed custom validity check")
			}
			if len(got) > 0 && !isUserTurnStart(got[0]) {
				t.Error("result starts with non-user-turn message")
			}
		})
	}
}

// TestPutSessionNeverOrphan is a property-style test verifying that for every
// cut point into a conversation slice, putSession never produces a session
// whose first message is a tool_result.
func TestPutSessionNeverOrphan(t *testing.T) {
	// Build a realistic alternating conversation with tool_use/tool_result pairs.
	conv := []anthropic.MessageParam{
		textMsg(),                  // 0 user
		assistantWithToolUse("t1"), // 1 assistant tool_use
		toolResultMsg("t1"),        // 2 user tool_result (orphan if first)
		assistantMsg(),             // 3 assistant text
		textMsg(),                  // 4 user text
		assistantWithToolUse("t2"), // 5 assistant tool_use
		toolResultMsg("t2"),        // 6 user tool_result
		assistantMsg(),             // 7 assistant text
		textMsg(),                  // 8 user text
	}

	a := &Agent{}
	id := snowflake.ID(999)

	for cut := 0; cut <= len(conv); cut++ {
		slice := conv[cut:]
		a.putSession(id, slice)
		got := a.getSession(id)
		if len(got) > 0 && !isUserTurnStart(got[0]) {
			t.Errorf("cut=%d: session starts with non-user-turn message", cut)
		}
	}
}
