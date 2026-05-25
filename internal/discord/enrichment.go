package discord

import (
	"fmt"
	"sync"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

type UserCache struct {
	mu      sync.RWMutex
	members []discord.Member
}

func (b *Bot) GetUsernameForID(id string) (string, error) {
	user, err := b.GetMemberForID(id)
	if err != nil {
		return "", err
	}
	return user.EffectiveName(), nil
}

func (b *Bot) onPresenceUpdate(event *events.PresenceUpdate) {
	b.presences.Store(event.Presence.PresenceUser.ID, event.Presence.Status)
}

func (b *Bot) onGuildVoiceStateUpdate(event *events.GuildVoiceStateUpdate) {
	state := event.VoiceState
	if state.ChannelID == nil {
		b.voiceChannels.Delete(state.UserID)
		return
	}
	ch, err := event.Client().Rest.GetChannel(*state.ChannelID)
	if err != nil {
		b.voiceChannels.Store(state.UserID, "")
		return
	}
	b.voiceChannels.Store(state.UserID, ch.Name())
}

func (b *Bot) OnlineMembers() []discord.Member {
	b.cache.mu.RLock()
	defer b.cache.mu.RUnlock()
	var result []discord.Member
	for _, m := range b.cache.members {
		val, ok := b.presences.Load(m.User.ID)
		if !ok {
			continue
		}
		s := val.(discord.OnlineStatus)
		if s != discord.OnlineStatusOffline && s != discord.OnlineStatusInvisible {
			result = append(result, m)
		}
	}
	return result
}

// ensureCache populates the member cache on first call. Discord requires an
// explicit limit or returns no results; 1000 is the API maximum.
func (b *Bot) ensureCache() error {
	b.cache.mu.RLock()
	initialized := b.cache.members != nil
	b.cache.mu.RUnlock()
	if initialized {
		return nil
	}

	b.cache.mu.Lock()
	defer b.cache.mu.Unlock()
	if b.cache.members == nil {
		fetched, err := b.client.Rest.GetMembers(snowflake.MustParse(b.config.DiscordGuild), 1000, 0)
		if err != nil {
			return err
		}
		b.cache.members = fetched
	}
	return nil
}

func (b *Bot) GetMemberForID(id string) (*discord.Member, error) {
	if err := b.ensureCache(); err != nil {
		return nil, err
	}

	b.cache.mu.RLock()
	defer b.cache.mu.RUnlock()
	for i, member := range b.cache.members {
		if member.User.ID.String() == id {
			return &b.cache.members[i], nil
		}
	}
	return nil, fmt.Errorf("member %s not found", id)
}

func (b *Bot) AllMembers() ([]discord.Member, error) {
	if err := b.ensureCache(); err != nil {
		return nil, err
	}
	b.cache.mu.RLock()
	defer b.cache.mu.RUnlock()
	return b.cache.members, nil
}

func (b *Bot) VoiceChannels() map[snowflake.ID]string {
	result := make(map[snowflake.ID]string)
	b.voiceChannels.Range(func(k, v any) bool {
		result[k.(snowflake.ID)] = v.(string)
		return true
	})
	return result
}
