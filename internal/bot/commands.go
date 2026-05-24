package bot

import (
	"context"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/leikonga/doofus-rick/internal/store"
)

var commands = []discord.ApplicationCommandCreate{
	discord.SlashCommandCreate{
		Name:        "ping",
		Description: "check if doofus-rick is alive",
	},
	discord.SlashCommandCreate{
		Name:        "quote",
		Description: "create a new quote",
	},
	discord.SlashCommandCreate{
		Name:        "randomquote",
		Description: "get a random quote",
	},
}

func (b *Bot) handlePingCommand(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
	return e.CreateMessage(discord.MessageCreate{
		Content: "pong!",
		Flags:   discord.MessageFlagEphemeral,
	})
}

func (b *Bot) handleQuote(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
	return e.Modal(discord.ModalCreate{
		CustomID: "/quote",
		Title:    "Time for a new quote",
		Components: []discord.LayoutComponent{
			discord.NewLabel("Content", discord.NewParagraphTextInput("content").WithRequired(true)),
			discord.NewLabel("Participants", discord.NewUserSelectMenu("participants", "").WithMaxValues(20)),
		},
	})
}

func (b *Bot) handleRandomQuote(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()

	quote, err := b.store.GetRandomQuote(ctx)
	if err != nil {
		slog.Warn("failed to get random quote", "error", err)
		return e.CreateMessage(discord.MessageCreate{
			Content: "no quotes found",
			Flags:   discord.MessageFlagEphemeral,
		})
	}
	author, err := b.GetMemberForID(quote.Creator)
	if err != nil {
		slog.Warn("failed to get author for quote", "error", err)
	}

	embed := discord.Embed{
		Description: quote.Content,
		Color:       0x11806A,
		Timestamp:   &quote.CreatedAt,
		Footer:      memberEmbedFooter(author, quote.Creator),
	}

	return e.CreateMessage(discord.MessageCreate{
		Embeds: []discord.Embed{embed},
	})
}

func (b *Bot) handleQuoteSubmission(e *handler.ModalEvent) error {
	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()

	content := e.Data.Text("content")

	users := e.Data.Users("participants")
	participants := make([]string, 0, len(users))
	for _, u := range users {
		participants = append(participants, u.ID.String())
	}

	creatorID := e.Member().User.ID.String()

	quote := store.Quote{
		Creator:      creatorID,
		Content:      content,
		Participants: participants,
	}

	if err := b.store.CreateQuote(ctx, quote); err != nil {
		slog.Error("failed to create quote", "error", err)
		return e.CreateMessage(discord.MessageCreate{
			Content: "seems like there was an issue creating the quote",
			Flags:   discord.MessageFlagEphemeral,
		})
	}

	author, err := b.GetMemberForID(creatorID)
	if err != nil {
		slog.Warn("failed to get author for quote", "error", err)
	}

	return e.CreateMessage(discord.MessageCreate{
		Embeds: []discord.Embed{
			{
				Description: content,
				Color:       0x11806A,
				Timestamp:   new(time.Now()),
				Footer:      memberEmbedFooter(author, creatorID),
			},
		},
	})
}

func memberEmbedFooter(member *discord.Member, fallbackID string) *discord.EmbedFooter {
	if member == nil {
		return &discord.EmbedFooter{Text: fallbackID}
	}
	return &discord.EmbedFooter{
		Text:    member.EffectiveName(),
		IconURL: member.User.EffectiveAvatarURL(),
	}
}
