package bot

import (
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

type CommandHandler func(s *discordgo.Session, i *discordgo.InteractionCreate)

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "ping",
		Description: "check if doofus-rick is alive",
	},
}

func (b *Bot) getCommandHandlers() map[string]CommandHandler {
	return map[string]CommandHandler{
		"ping": b.handlePingCommand,
	}
}

func (b *Bot) handlePingCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "pong!",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		slog.Error("failed to respond to ping command", "error", err)
	}
}
