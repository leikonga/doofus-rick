package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
)

type mockDiscord struct {
	users      map[string]string
	statuses   map[string]discord.OnlineStatus
	voice      map[string]string
	activities map[string][]discord.Activity
}

func (m *mockDiscord) GetMemberForID(_ string) (*discord.Member, error) { return nil, nil }
func (m *mockDiscord) GetUsernameForID(id string) (string, error) {
	if name, ok := m.users[id]; ok {
		return name, nil
	}
	return "", nil
}
func (m *mockDiscord) OnlineMembers() []discord.Member        { return nil }
func (m *mockDiscord) AllMembers() ([]discord.Member, error)  { return nil, nil }
func (m *mockDiscord) VoiceChannels() map[snowflake.ID]string { return nil }
func (m *mockDiscord) VoiceChannelForID(id string) string     { return m.voice[id] }
func (m *mockDiscord) GetStatusForID(id string) discord.OnlineStatus {
	return m.statuses[id]
}
func (m *mockDiscord) GetActivitiesForID(id string) []discord.Activity { return m.activities[id] }

func newTestAgent(users map[string]string) *Agent {
	return &Agent{discord: &mockDiscord{users: users}}
}

func assistantMsg() llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.TextPart("response")}}
}

func TestBuildTranscript(t *testing.T) {
	msgs := []discord.Message{
		{
			ID:        snowflake.ID(1),
			Author:    discord.User{ID: snowflake.ID(100), Username: "alice"},
			Content:   "hello",
			CreatedAt: time.Now(),
		},
		{
			ID:        snowflake.ID(2),
			Author:    discord.User{ID: snowflake.ID(200), Username: "bob"},
			Content:   "hi",
			CreatedAt: time.Now(),
		},
		{
			ID:        snowflake.ID(3),
			Author:    discord.User{ID: snowflake.ID(300), Username: "rick"},
			Content:   "yo",
			CreatedAt: time.Now(),
		},
	}

	a := newTestAgent(map[string]string{
		"100": "alice",
		"200": "bob",
		"300": "rick",
	})

	got := buildTranscript(snowflake.ID(300), snowflake.ID(1), msgs, a.memberName)

	if len(got) != 1 {
		t.Errorf("expected 1 message, got %d", len(got))
	}

	if got[0].Role != llm.RoleUser {
		t.Errorf("expected user role, got %s", got[0].Role)
	}

	content := partsText(got[0].Parts)
	if len(content) == 0 {
		t.Error("expected content in message")
	}

	if !strings.Contains(content[0], "bob") {
		t.Error("should contain bob's message")
	}

	if strings.Contains(content[0], "alice") {
		t.Error("should not contain alice's message (skipped)")
	}

	if strings.Contains(content[0], "rick") {
		t.Error("should not contain rick's message (bot)")
	}
}

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

func TestMessageText(t *testing.T) {
	tests := []struct {
		name string
		msg  llm.Message
		want string
	}{
		{"no parts", llm.Message{Role: llm.RoleAssistant}, ""},
		{"single text part", assistantMsg(), "response"},
		{
			"multiple text parts concatenated",
			llm.Message{Parts: []llm.ContentPart{llm.TextPart("a"), llm.TextPart("b")}},
			"ab",
		},
		{
			"non-text parts ignored",
			llm.Message{Parts: []llm.ContentPart{llm.ImagePart("http://x"), llm.TextPart("caption")}},
			"caption",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := messageText(tc.msg); got != tc.want {
				t.Errorf("messageText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPartsText(t *testing.T) {
	tests := []struct {
		name  string
		parts []llm.ContentPart
		want  []string
	}{
		{"empty parts", []llm.ContentPart{}, []string{}},
		{"single text", []llm.ContentPart{llm.TextPart("hello")}, []string{"hello"}},
		{"multiple text", []llm.ContentPart{llm.TextPart("a"), llm.TextPart("b")}, []string{"a", "b"}},
		{"mixed parts", []llm.ContentPart{llm.ImagePart("http://x"), llm.TextPart("caption")}, []string{"caption"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := partsText(tc.parts)
			if len(got) != len(tc.want) {
				t.Errorf("got %d parts, want %d", len(got), len(tc.want))
				return
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("part %d: got %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

func TestBuildCachedPrefix(t *testing.T) {
	roster := "<users>...\n</users>"
	got := buildCachedPrefix(roster, "general", "chat")
	if !strings.Contains(got, "<users>") {
		t.Error("expected roster in cached prefix")
	}
	if !strings.Contains(got, "# channel: general") {
		t.Error("expected channel in cached prefix")
	}
	if !strings.Contains(got, "# topic: chat") {
		t.Error("expected topic in cached prefix")
	}
	if strings.Contains(got, "<now>") {
		t.Error("should not contain timestamp (volatile)")
	}
}

func TestBuildUncachedTail(t *testing.T) {
	got := buildUncachedTail("", "")
	if !strings.Contains(got, "<now>") {
		t.Error("expected timestamp in uncached tail")
	}
}

func TestBuildUncachedTail_IncludesGradDoAndRecall(t *testing.T) {
	got := buildUncachedTail("<grad do>\nsnowflake=1 status=online\n</grad do>", "<recall>\nsome context\n</recall>\n")
	if !strings.Contains(got, "<grad do>") {
		t.Error("expected grad do block in uncached tail")
	}
	if !strings.Contains(got, "<recall>") {
		t.Error("expected recall block in uncached tail")
	}
}

func TestMemberName(t *testing.T) {
	a := newTestAgent(map[string]string{
		"123": "alice",
	})

	tests := []struct {
		user discord.User
		want string
	}{
		{discord.User{ID: snowflake.ID(123), Username: "alice"}, "alice"},
		{discord.User{ID: snowflake.ID(456), Username: "bob"}, "bob"},
	}

	for _, tc := range tests {
		t.Run(tc.user.Username, func(t *testing.T) {
			got := a.memberName(tc.user)
			if got != tc.want {
				t.Errorf("memberName() = %q, want %q", got, tc.want)
			}
		})
	}
}
