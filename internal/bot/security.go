package bot

import "github.com/disgoorg/snowflake/v2"

func (b *Bot) IsGuildMember(id string) (bool, error) {
	_, err := b.client.Rest.GetMember(snowflake.MustParse(b.config.DiscordGuild), snowflake.MustParse(id))
	if err != nil {
		return false, err
	}
	return true, nil
}
