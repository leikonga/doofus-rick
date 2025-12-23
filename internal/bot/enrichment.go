package bot

import (
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
)

type UserCache struct {
	mu    sync.RWMutex
	names map[string]string
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
	for _, member := range b.guild.Members {
		if member.User.ID == id {
			return member, nil
		}
	}
	return nil, fmt.Errorf("member %s not found", id)
}
