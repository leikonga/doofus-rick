package bot

func (b *Bot) IsGuildMember(id string) (bool, error) {
	member, err := b.dg.GuildMember(b.config.DiscordGuild, id)
	if err != nil {
		return false, err
	}
	return member != nil, nil
}
