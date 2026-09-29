package discord

import (
	"fmt"

	"github.com/disgoorg/snowflake/v2"
)

func (b *Bot) IsGuildMember(id string) (bool, error) {
	_, err := b.client.Rest.GetMember(snowflake.MustParse(b.config.DiscordGuild), snowflake.MustParse(id))
	if err != nil {
		return false, fmt.Errorf("get guild member %s: %w", id, err)
	}
	return true, nil
}
