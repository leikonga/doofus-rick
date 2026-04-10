package bot

import (
	"fmt"
	"sync"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

type UserCache struct {
	mu      sync.RWMutex
	members []discord.Member
}

var cache = &UserCache{}

func (b *Bot) GetUsernameForID(id string) (string, error) {
	user, err := b.GetMemberForID(id)
	if err != nil {
		return "", err
	}

	return user.EffectiveName(), nil
}

func (b *Bot) GetMemberForID(id string) (*discord.Member, error) {
	cache.mu.Lock()
	if cache.members == nil {
		// set 1000 user limit, because discord will not return any users if limit is not set
		fetched, err := b.client.Rest.GetMembers(snowflake.MustParse(b.config.DiscordGuild), 1000, 0)
		if err != nil {
			cache.mu.Unlock()
			return nil, err
		}
		cache.members = fetched
	}
	cache.mu.Unlock()
	cache.mu.RLock()
	for i, member := range cache.members {
		if member.User.ID.String() == id {
			cache.mu.RUnlock()
			return &cache.members[i], nil
		}
	}
	cache.mu.RUnlock()
	return nil, fmt.Errorf("member %s not found", id)
}
