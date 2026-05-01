package bot

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

func (b *Bot) onlineMembers() []discord.Member {
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

func (b *Bot) GetMemberForID(id string) (*discord.Member, error) {
	b.cache.mu.RLock()
	initialized := b.cache.members != nil
	b.cache.mu.RUnlock()

	if !initialized {
		b.cache.mu.Lock()
		if b.cache.members == nil {
			// set 1000 user limit, because discord will not return any users if limit is not set
			fetched, err := b.client.Rest.GetMembers(snowflake.MustParse(b.config.DiscordGuild), 1000, 0)
			if err != nil {
				b.cache.mu.Unlock()
				return nil, err
			}
			b.cache.members = fetched
		}
		b.cache.mu.Unlock()
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
