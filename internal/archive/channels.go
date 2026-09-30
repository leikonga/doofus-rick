package archive

import (
	"context"
	"sync"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

type channelGetter interface {
	GetChannel(channelID snowflake.ID, opts ...rest.RequestOpt) (discord.Channel, error)
}

type ChannelNamer interface {
	ChannelName(ctx context.Context, channelID uint64) string
}

// Lookup failures are cached as empty names so deleted channels are not re-fetched for every chunk.
type ChannelNames struct {
	rest  channelGetter
	mu    sync.Mutex
	names map[uint64]string
}

func NewChannelNames(rest channelGetter) *ChannelNames {
	return &ChannelNames{rest: rest, names: make(map[uint64]string)}
}

func (n *ChannelNames) ChannelName(ctx context.Context, channelID uint64) string {
	n.mu.Lock()
	name, ok := n.names[channelID]
	n.mu.Unlock()
	if ok {
		return name
	}

	ch, err := n.rest.GetChannel(snowflake.ID(channelID), rest.WithCtx(ctx))
	if err == nil && ch != nil {
		name = ch.Name()
	}

	if ctx.Err() != nil {
		return ""
	}
	n.mu.Lock()
	n.names[channelID] = name
	n.mu.Unlock()
	return name
}

type NoChannelNames struct{}

func (NoChannelNames) ChannelName(context.Context, uint64) string { return "" }
