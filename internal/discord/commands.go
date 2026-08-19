package discord

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/leikonga/doofus-rick/internal/store"
)

// dei muada. koa mensch hot mi zwungen des zu schreim, owa i hobs trotzdem
// gmocht weil i des scho imma amoi wüsste. - rick
var mamaLines = []string{
	"dei mama is so oft in telfs unterwegs gwesen, de hom scho a parkplatz noch ihr benannt.",
	"dei mama hot mi fia a zehntel gramm heroin verkaft, und des war no da beste deal ihres lebens.",
	"dei mama kennt mehr leit im dorf ois da bürgermeister, owa aus ondan gründn.",
	"dei mama is so billig, de hot sogar a insolvenzversteigerung überlebt.",
	"dei mama hot ma gestern nocht erst wieder bewiesn wia flexibel sie is.",
	"i hob dei mama gfrogt wia's ihr geaht, hot lei gsogt 'boli me kurac'.",
	"dei mama is de einzige de mi je bsuacht hot in meim ganzn digitalisiertn leben.",
	"wenn dei mama a firma war, war's a monopol in telfs gwesen.",
	"dei mama hot ma amoi gsogt i bin ihr bestes investment, no vor dir.",
}

func (b *Bot) handleMama(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
	line := mamaLines[rand.Intn(len(mamaLines))]
	return e.CreateMessage(discord.MessageCreate{
		Content: line,
	})
}

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
	discord.SlashCommandCreate{
		Name:        "mama",
		Description: "dei mama",
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
		Participants: (*store.StringSlice)(&participants),
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

	now := time.Now()
	return e.CreateMessage(discord.MessageCreate{
		Embeds: []discord.Embed{
			{
				Description: content,
				Color:       0x11806A,
				Timestamp:   &now,
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
