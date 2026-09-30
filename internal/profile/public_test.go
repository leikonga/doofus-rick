package profile

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

const testGuild = snowflake.ID(1000)

type fakeGuild struct {
	channels []discord.GuildChannel
	roles    []discord.Role
	chErr    error
	calls    int
}

func (f *fakeGuild) GetGuildChannels(snowflake.ID, ...rest.RequestOpt) ([]discord.GuildChannel, error) {
	f.calls++
	return f.channels, f.chErr
}

func (f *fakeGuild) GetRoles(snowflake.ID, ...rest.RequestOpt) ([]discord.Role, error) {
	return f.roles, nil
}

func textChannel(t *testing.T, id snowflake.ID, overwrites ...discord.PermissionOverwrite) discord.GuildChannel {
	return channelFromJSON(t, id, discord.ChannelTypeGuildText, overwrites)
}

func categoryChannel(t *testing.T, id snowflake.ID) discord.GuildChannel {
	return channelFromJSON(t, id, discord.ChannelTypeGuildCategory, nil)
}

func channelFromJSON(t *testing.T, id snowflake.ID, typ discord.ChannelType, overwrites []discord.PermissionOverwrite) discord.GuildChannel {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"id": id, "type": typ, "permission_overwrites": overwrites})
	if err != nil {
		t.Fatal(err)
	}
	var ch discord.UnmarshalChannel
	if err := json.Unmarshal(raw, &ch); err != nil {
		t.Fatal(err)
	}
	return ch.Channel.(discord.GuildChannel)
}

func everyoneOverwrite(allow, deny discord.Permissions) discord.PermissionOverwrite {
	return discord.RolePermissionOverwrite{RoleID: testGuild, Allow: allow, Deny: deny}
}

func TestPublicChannelIDs(t *testing.T) {
	g := &fakeGuild{
		roles: []discord.Role{{ID: testGuild, Permissions: discord.PermissionViewChannel}},
		channels: []discord.GuildChannel{
			textChannel(t, 1),
			textChannel(t, 2, everyoneOverwrite(0, discord.PermissionViewChannel)),
			textChannel(t, 3, everyoneOverwrite(discord.PermissionViewChannel, discord.PermissionViewChannel)),
			textChannel(t, 4, discord.RolePermissionOverwrite{RoleID: 55, Deny: discord.PermissionViewChannel}),
			categoryChannel(t, 5),
		},
	}
	p, err := NewPublicChannels(g, "1000")
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.PublicChannelIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint64{1, 3, 4}; !slices.Equal(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}

func TestPublicChannelIDsBaseWithoutView(t *testing.T) {
	g := &fakeGuild{
		roles: []discord.Role{{ID: testGuild, Permissions: 0}},
		channels: []discord.GuildChannel{
			textChannel(t, 1),
			textChannel(t, 2, everyoneOverwrite(discord.PermissionViewChannel, 0)),
		},
	}
	p, _ := NewPublicChannels(g, "1000")
	got, err := p.PublicChannelIDs(context.Background())
	if err != nil || !slices.Equal(got, []uint64{2}) {
		t.Errorf("ids = %v, %v, want [2]", got, err)
	}
}

func TestPublicChannelIDsCachesAndExpires(t *testing.T) {
	g := &fakeGuild{roles: []discord.Role{{ID: testGuild, Permissions: discord.PermissionViewChannel}}, channels: []discord.GuildChannel{textChannel(t, 1)}}
	p, _ := NewPublicChannels(g, "1000")
	now := time.Now()
	p.now = func() time.Time { return now }

	for range 3 {
		if _, err := p.PublicChannelIDs(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if g.calls != 1 {
		t.Errorf("calls = %d, want 1 within ttl", g.calls)
	}
	now = now.Add(publicChannelsTTL + time.Second)
	if _, err := p.PublicChannelIDs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.calls != 2 {
		t.Errorf("calls = %d, want 2 after ttl", g.calls)
	}
}

func TestPublicChannelIDsErrors(t *testing.T) {
	g := &fakeGuild{chErr: errors.New("boom")}
	p, _ := NewPublicChannels(g, "1000")
	if _, err := p.PublicChannelIDs(context.Background()); err == nil {
		t.Error("expected channel listing error")
	}

	g = &fakeGuild{roles: []discord.Role{{ID: 1, Permissions: discord.PermissionViewChannel}}}
	p, _ = NewPublicChannels(g, "1000")
	if _, err := p.PublicChannelIDs(context.Background()); err == nil {
		t.Error("expected missing @everyone role error")
	}

	if _, err := NewPublicChannels(g, "nope"); err == nil {
		t.Error("expected invalid guild id error")
	}
}
