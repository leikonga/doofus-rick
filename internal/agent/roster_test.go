package agent

import (
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
)

func TestBuildLeitBlock(t *testing.T) {
	got := buildLeitBlock([]rosterEntry{
		{id: 1, name: "klaus", affinity: 40},
		{id: 2, name: "josef", affinity: -10, reason: "roasted rick"},
	})

	if !strings.HasPrefix(got, "<leit>\n") || !strings.HasSuffix(got, "</leit>") {
		t.Fatalf("expected <leit> wrapper, got %q", got)
	}
	if !strings.Contains(got, "snowflake=1 name=klaus affinity=40") {
		t.Errorf("missing klaus entry: %q", got)
	}
	if !strings.Contains(got, `snowflake=2 name=josef affinity=-10 reason="roasted rick"`) {
		t.Errorf("missing josef entry with reason: %q", got)
	}
}

func TestBuildGradDoBlock_SkipsOfflineMembers(t *testing.T) {
	a := &Agent{discord: &mockDiscord{
		statuses: map[string]discord.OnlineStatus{
			"1": discord.OnlineStatusOnline,
			"2": discord.OnlineStatusOffline,
		},
	}}

	got := a.buildGradDoBlock([]rosterEntry{
		{id: 1, name: "klaus"},
		{id: 2, name: "josef"},
	})

	if !strings.Contains(got, "snowflake=1 status=online") {
		t.Errorf("expected online member in grad do block: %q", got)
	}
	if strings.Contains(got, "snowflake=2") {
		t.Errorf("offline member should not appear in grad do block: %q", got)
	}
}

func TestBuildGradDoBlock_EmptyWhenNobodyOnline(t *testing.T) {
	a := &Agent{discord: &mockDiscord{}}
	got := a.buildGradDoBlock([]rosterEntry{{id: 1, name: "klaus"}})
	if got != "" {
		t.Fatalf("expected empty grad do block when nobody is online, got %q", got)
	}
}

func TestBuildGradDoBlock_IncludesVoiceAndActivity(t *testing.T) {
	a := &Agent{discord: &mockDiscord{
		statuses: map[string]discord.OnlineStatus{"1": discord.OnlineStatusOnline},
		voice:    map[string]string{"1": "general-vc"},
		activities: map[string][]discord.Activity{
			"1": {{Name: "Elden Ring"}},
		},
	}}

	got := a.buildGradDoBlock([]rosterEntry{{id: 1, name: "klaus"}})
	if !strings.Contains(got, "vc=general-vc") {
		t.Errorf("expected voice channel in grad do block: %q", got)
	}
	if !strings.Contains(got, "activity=Elden Ring") {
		t.Errorf("expected activity in grad do block: %q", got)
	}
}

func TestMemberCanSeeChannel_NoOverwritesAllowsEveryone(t *testing.T) {
	if !memberCanSeeChannel(discord.Member{}, nil) {
		t.Fatal("expected no overwrites to allow visibility")
	}
}
