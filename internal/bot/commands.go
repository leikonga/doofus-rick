package bot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/konga-dev/doofus-rick/internal/store"
	"gorm.io/gorm"
)

type CommandHandler func(s *discordgo.Session, i *discordgo.InteractionCreate)

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "ping",
		Description: "check if doofus-rick is alive",
	},
	{
		Name:        "quote",
		Description: "create a new quote",
	},
	{
		Name:        "randomquote",
		Description: "get a random quote",
	},
}

func (b *Bot) getCommandHandlers() map[string]CommandHandler {
	return map[string]CommandHandler{
		"ping":        b.handlePingCommand,
		"quote":       b.handleQuote,
		"randomquote": b.handleRandomQuote,
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

func (b *Bot) handleQuote(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "quote",
			Title:    "Time for a new quote",
			Flags:    discordgo.MessageFlagsIsComponentsV2,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							Label:    "Content",
							CustomID: "content",
							Style:    discordgo.TextInputParagraph,
							Value:    "",
							Required: true,
						},
					},
				},
				discordgo.Label{
					Label: "Participants",
					Component: discordgo.SelectMenu{
						MenuType:    discordgo.UserSelectMenu,
						CustomID:    "participants",
						Placeholder: "",
						MaxValues:   20,
					},
				},
			},
		},
	})

	if err != nil {
		slog.Error("failed to respond to quote command", "error", err)
	}
}

func (b *Bot) handleRandomQuote(s *discordgo.Session, i *discordgo.InteractionCreate) {
	quote := b.store.GetRandomQuote()
	author, err := b.GetUserForID(quote.Creator)

	if err != nil {
		author = &discordgo.User{ID: quote.Creator}
	}

	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{
				{
					Description: quote.Content,
					Color:       0x11806A,
					Timestamp:   fmt.Sprint(quote.Timestamp.Format(time.RFC3339)),
					Footer: &discordgo.MessageEmbedFooter{
						Text:    author.Username,
						IconURL: author.AvatarURL("64x64"),
					},
				},
			},
		},
	})

	if err != nil {
		slog.Error("failed to send random quote to channel", "error", err)
	}
}

func (b *Bot) handleQuoteSubmission(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionModalSubmit:
		data := i.ModalSubmitData()

		if data.CustomID != "quote" {
			return
		}

		content := data.Components[0].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value
		var participants []string
		for _, element := range data.Components[1].(*discordgo.Label).Component.(*discordgo.SelectMenu).Values {
			participants = append(participants, element)
		}

		quote := &store.Quote{
			Creator:      i.Member.User.ID,
			Content:      content,
			Participants: participants,
		}

		err := gorm.G[store.Quote](b.store.Db()).Create(context.Background(), quote)

		if err != nil {
			slog.Error("failed to create quote", "error", err)

			err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "seems like there was an issue creating the quote",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})

			if err != nil {
				slog.Error("failed to respond to quote command", "error", err)
			}
		}

		err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{
					{
						Description: content,
						Color:       0x11806A,
						Timestamp:   fmt.Sprint(time.Now().Format(time.RFC3339)),
						Footer: &discordgo.MessageEmbedFooter{
							Text:    i.Member.User.DisplayName(),
							IconURL: i.Member.User.AvatarURL("64x64"),
						},
					},
				},
			},
		})

		if err != nil {
			slog.Error("failed to send new quote to channel", "error", err)
		}
	}
}
