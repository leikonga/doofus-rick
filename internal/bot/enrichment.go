package bot

import (
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
)

type UserCache struct {
	mu      sync.RWMutex
	members []*discordgo.Member
}

var cache = &UserCache{}

func (b *Bot) GetUsernameForID(id string) (string, error) {
	user, err := b.GetMemberForID(id)
	if err != nil {
		return "", err
	}

	return user.DisplayName(), nil
}

func (b *Bot) GetMemberForID(id string) (*discordgo.Member, error) {
	cache.mu.Lock()
	if cache.members == nil {
		// set 1000 user limit, because discord will not return any users if limit is not set
		fetched, err := b.dg.GuildMembers(b.config.DiscordGuild, "0", 1000)
		if err != nil {
			cache.mu.Unlock()
			return nil, err
		}
		cache.members = fetched
	}
	cache.mu.Unlock()
	cache.mu.RLock()
	for _, member := range cache.members {
		if member.User.ID == id {
			cache.mu.RUnlock()
			return member, nil
		}
	}
	cache.mu.RUnlock()
	return nil, fmt.Errorf("member %s not found", id)
}
