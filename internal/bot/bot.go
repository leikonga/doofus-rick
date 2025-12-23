package bot

import (
	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/konga-dev/doofus-rick/internal/config"
	"github.com/konga-dev/doofus-rick/internal/store"
)

type Bot struct {
	store  *store.Store
	config *config.Config
	dg     *discordgo.Session
	guild  *discordgo.Guild
}

func New(s *store.Store, c *config.Config) *Bot {
	return &Bot{store: s, config: c}
}

func (b *Bot) Run() error {
	dg, _ := discordgo.New("Bot " + b.config.DiscordToken)

	dg.AddHandler(b.handleInteraction)
	err := dg.Open()
	if err != nil {
		return err
	}
	b.dg = dg

	if b.config.DiscordGuild == "" {
		slog.Warn("no discord guild configured, skipping command registration")
	} else {
		registeredCommands, err := dg.ApplicationCommands(dg.State.User.ID, b.config.DiscordGuild)
		if err != nil {
			slog.Error("failed to fetch registered commands", "error", err)
		}
	outer:
		for _, v := range commands {
			for _, cmd := range registeredCommands {
				if cmd.Name == v.Name {
					_, err := dg.ApplicationCommandEdit(dg.State.User.ID, b.config.DiscordGuild, cmd.ID, v)
					if err != nil {
						slog.Error("failed to edit command", "error", err)
					}
					continue outer
				}
			}

			_, err := dg.ApplicationCommandCreate(dg.State.User.ID, b.config.DiscordGuild, v)
			if err != nil {
				slog.Error("failed to register command", "error", err)
			}
		}
		b.guild, err = b.dg.Guild(b.config.DiscordGuild)
		if err != nil {
			return err
		}
	}

	slog.Info("connected to discord", "userid", dg.State.User.ID, "guilds", len(dg.State.Guilds))
	return nil
}

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		handlers := b.getCommandHandlers()
		if handler, ok := handlers[i.ApplicationCommandData().Name]; ok {
			handler(s, i)
		}
		break

	case discordgo.InteractionModalSubmit:
		b.handleQuoteSubmission(s, i)
		break
	}
}
