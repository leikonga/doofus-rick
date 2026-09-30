package profile

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

const publicChannelsTTL = 10 * time.Minute

type guildReader interface {
	GetGuildChannels(guildID snowflake.ID, opts ...rest.RequestOpt) ([]discord.GuildChannel, error)
	GetRoles(guildID snowflake.ID, opts ...rest.RequestOpt) ([]discord.Role, error)
}

// PublicChannels lists message channels where @everyone can view the channel. The @everyone role id equals the guild id.
type PublicChannels struct {
	rest    guildReader
	guildID snowflake.ID
	ttl     time.Duration
	now     func() time.Time

	mu      sync.Mutex
	ids     []uint64
	fetched time.Time
}

func NewPublicChannels(rest guildReader, guildID string) (*PublicChannels, error) {
	id, err := snowflake.Parse(guildID)
	if err != nil {
		return nil, fmt.Errorf("parse guild id %q: %w", guildID, err)
	}
	return &PublicChannels{rest: rest, guildID: id, ttl: publicChannelsTTL, now: time.Now}, nil
}

func (p *PublicChannels) PublicChannelIDs(ctx context.Context) ([]uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ids != nil && p.now().Sub(p.fetched) < p.ttl {
		return p.ids, nil
	}

	channels, err := p.rest.GetGuildChannels(p.guildID, rest.WithCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("list guild channels: %w", err)
	}
	roles, err := p.rest.GetRoles(p.guildID, rest.WithCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("list guild roles: %w", err)
	}
	base, ok := everyonePermissions(roles, p.guildID)
	if !ok {
		return nil, fmt.Errorf("@everyone role not found")
	}

	ids := []uint64{}
	for _, ch := range channels {
		gmc, isMessageChannel := ch.(discord.GuildMessageChannel)
		if !isMessageChannel {
			continue
		}
		if everyoneCanView(base, gmc.PermissionOverwrites(), p.guildID) {
			ids = append(ids, uint64(ch.ID()))
		}
	}
	p.ids, p.fetched = ids, p.now()
	return ids, nil
}

func everyonePermissions(roles []discord.Role, guildID snowflake.ID) (discord.Permissions, bool) {
	for _, r := range roles {
		if r.ID == guildID {
			return r.Permissions, true
		}
	}
	return 0, false
}

func everyoneCanView(base discord.Permissions, overwrites discord.PermissionOverwrites, guildID snowflake.ID) bool {
	perms := base
	if ow, ok := overwrites.Role(guildID); ok {
		perms = perms.Remove(ow.Deny).Add(ow.Allow)
	}
	return perms.Has(discord.PermissionViewChannel)
}
