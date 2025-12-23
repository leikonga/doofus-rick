package bot

import (
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
)

type UserCache struct {
	mu      sync.RWMutex
	names   map[string]string
	members []*discordgo.Member
}

var cache = &UserCache{names: make(map[string]string)}

func (b *Bot) GetUsernameForID(id string) (string, error) {
	cache.mu.RLock()
	name, ok := cache.names[id]
	cache.mu.RUnlock()
	if ok {
		return name, nil
	}

	user, err := b.GetMemberForID(id)
	if err != nil {
		return "", err
	}

	cache.mu.Lock()
	cache.names[id] = user.DisplayName()
	cache.mu.Unlock()

	return user.DisplayName(), nil
}

func (b *Bot) GetMemberForID(id string) (*discordgo.Member, error) {
	cache.mu.RLock()
	members := cache.members
	cache.mu.RUnlock()

	if members == nil {
		cache.mu.Lock()
		if cache.members == nil {
			fetched, err := b.dg.GuildMembers(b.config.DiscordGuild, "", 0)
			if err != nil {
				cache.mu.Unlock()
				return nil, err
			}
			cache.members = fetched
			members = fetched
		} else {
			members = cache.members
		}
		cache.mu.Unlock()
	}

	for _, member := range members {
		if member.User.ID == id {
			return member, nil
		}
	}
	return nil, fmt.Errorf("member %s not found", id)
}
