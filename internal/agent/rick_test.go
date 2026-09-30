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

func TestBuildHistory(t *testing.T) {
	msgs := []discord.Message{
		{ID: 1, Author: discord.User{ID: 100, Username: "alice"}, Content: "trigger", CreatedAt: time.Now()},
		{ID: 2, Author: discord.User{ID: 200, Username: "bob"}, Content: "hi", CreatedAt: time.Now()},
		{ID: 3, Author: discord.User{ID: 300, Username: "rick"}, Content: "dei muada", CreatedAt: time.Now()},
		{ID: 4, Author: discord.User{ID: 400, Username: "mee6", Bot: true}, Content: "level up", CreatedAt: time.Now()},
		{ID: 5, Author: discord.User{ID: 200, Username: "bob"}, Content: "/mama", CreatedAt: time.Now()},
	}
	a := newTestAgent(map[string]string{"100": "alice", "200": "bob"})

	got := buildHistory(snowflake.ID(300), snowflake.ID(1), msgs, a.memberName)

	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(got), got)
	}
	if !strings.HasSuffix(got[0], " bob]: hi") {
		t.Errorf("line 0 = %q, want bob's message", got[0])
	}
	if !strings.HasSuffix(got[1], " rick (du)]: dei muada") {
		t.Errorf("line 1 = %q, want rick's own reply labelled as him", got[1])
	}
	if !strings.HasSuffix(got[2], " mee6 (bot)]: level up") {
		t.Errorf("line 2 = %q, want foreign bot labelled", got[2])
	}
}

func TestBuildVolatileTurn_TriggerLastAndVolatileContextInside(t *testing.T) {
	now := time.Date(2026, 9, 28, 14, 36, 0, 0, time.UTC)
	got := buildVolatileTurn(now, "<vitals>uptime=3h12m goroutines=41 heap_mb=78 gc_cycles=120</vitals>", "<grad do>\nsnowflake=1 status=online\n</grad do>", "<recall>\nold\n</recall>\n",
		[]string{"[14:32 klaus]: i hob trainiert"}, "[14:35 hans]: rick wos sogst")

	for _, want := range []string{"<now>2026-09-28 14:36 UTC</now>\n<vitals>uptime=3h12m goroutines=41 heap_mb=78 gc_cycles=120</vitals>\n<grad do>", "<recall>", "<verlauf>\n[14:32 klaus]: i hob trainiert\n</verlauf>"} {
		if !strings.Contains(got, want) {
			t.Errorf("turn missing %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "</kontext>\n<nachricht>\n[14:35 hans]: rick wos sogst\n</nachricht>") {
		t.Errorf("trigger is not the last block:\n%s", got)
	}
}

func TestBuildVolatileTurn_OmitsEmptySections(t *testing.T) {
	got := buildVolatileTurn(time.Now(), "", "", "", nil, "[hans]: (pinged Rick)")
	for _, absent := range []string{"<vitals>", "<grad do>", "<recall>", "<verlauf>"} {
		if strings.Contains(got, absent) {
			t.Errorf("turn should not contain %q:\n%s", absent, got)
		}
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

func TestBuildCachedPrefix(t *testing.T) {
	roster := "<leit>\nsnowflake=1 name=hans affinity=-20\n</leit>"
	got := buildCachedPrefix("<selbst>\ncommit=0123456\n</selbst>", roster, "999", "general", "chat")
	if !strings.HasPrefix(got, "<selbst>\ncommit=0123456\n</selbst>\n\n<leit>") {
		t.Errorf("expected <selbst> first, then <leit>:\n%s", got)
	}
	if !strings.Contains(got, "# channel: general (id: 999)") {
		t.Error("expected channel and id in cached prefix")
	}
	if !strings.Contains(got, "# topic: chat") {
		t.Error("expected topic in cached prefix")
	}
	if strings.Contains(got, "<now>") {
		t.Error("should not contain timestamp (volatile)")
	}
}

func TestBuildCachedPrefix_SkipsEmptyBlocks(t *testing.T) {
	tests := []struct {
		name, selbst, roster, channel, want string
	}{
		{"all empty", "", "", "", ""},
		{"selbst only", "<selbst></selbst>", "", "", "<selbst></selbst>"},
		{"roster only", "", "<leit></leit>", "", "<leit></leit>"},
		{"selbst and channel", "<selbst></selbst>", "", "general", "<selbst></selbst>\n\n# channel: general (id: )"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildCachedPrefix(tc.selbst, tc.roster, "", tc.channel, ""); got != tc.want {
				t.Errorf("buildCachedPrefix() = %q, want %q", got, tc.want)
			}
		})
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

func TestEscalateForCode(t *testing.T) {
	tests := []struct {
		name       string
		escalated  bool
		calls      []llm.ToolCall
		wantResult bool
	}{
		{"no calls, not escalated", false, nil, false},
		{"non-code call, not escalated", false, []llm.ToolCall{{Name: "web_search"}}, false},
		{"code call escalates", false, []llm.ToolCall{{Name: "code_read"}}, true},
		{"mixed calls escalate", false, []llm.ToolCall{{Name: "web_search"}, {Name: "code_edit"}}, true},
		{"already escalated persists with no calls", true, nil, true},
		{"already escalated persists with non-code calls", true, []llm.ToolCall{{Name: "web_search"}}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := escalateForCode(tc.escalated, tc.calls)
			if got != tc.wantResult {
				t.Errorf("escalateForCode(%v, %v) = %v, want %v", tc.escalated, tc.calls, got, tc.wantResult)
			}
		})
	}
}
