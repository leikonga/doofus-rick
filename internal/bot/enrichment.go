package bot

import "sync"

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

	user, err := b.dg.User(id)
	if err != nil {
		return "", err
	}

	cache.mu.Lock()
	cache.names[id] = user.Username
	cache.mu.Unlock()

	return user.Username, nil
}
