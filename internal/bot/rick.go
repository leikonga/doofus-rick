package bot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

const (
	maxContextLen = 500
	maxTokens     = int64(512)
	historyLimit  = 11
	rickFallback  = "najo woas i etz ned"
)

func (b *Bot) onMentionCreate(event *events.MessageCreate) {
	if event.Message.Author.Bot {
		return
	}

	botID := event.Client().ID()
	mentioned := false
	for _, u := range event.Message.Mentions {
		if u.ID == botID {
			mentioned = true
			break
		}
	}
	if !mentioned {
		return
	}

	systemPrompt, err := os.ReadFile(b.config.SystemPromptFile)
	if err != nil {
		slog.Warn("failed to read system prompt file", "error", err, "path", b.config.SystemPromptFile)
		b.replyFallback(event)
		return
	}

	msgs, err := event.Client().Rest.GetMessages(event.ChannelID, 0, 0, 0, historyLimit)
	if err != nil {
		slog.Warn("failed to fetch channel history", "error", err)
		b.replyFallback(event)
		return
	}
	slices.Reverse(msgs)

	var lines []string
	for _, msg := range msgs {
		if msg.ID == event.MessageID {
			continue
		}
		if msg.Author.Bot {
			continue
		}
		if strings.HasPrefix(msg.Content, "/") {
			continue
		}
		if len(msg.Content) > maxContextLen {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s]: %s", b.memberName(msg.Author), msg.Content))
	}

	triggerContent := strings.TrimSpace(
		strings.NewReplacer(
			fmt.Sprintf("<@%s>", botID), "",
			fmt.Sprintf("<@!%s>", botID), "",
		).Replace(event.Message.Content),
	)
	if triggerContent != "" {
		lines = append(lines, fmt.Sprintf("[%s]: %s", b.memberName(event.Message.Author), triggerContent))
	}

	response, err := b.callClaude(context.Background(), string(systemPrompt), strings.Join(lines, "\n"))
	if err != nil {
		slog.Warn("claude api call failed", "error", err)
		b.replyFallback(event)
		return
	}

	_, err = event.Client().Rest.CreateMessage(event.ChannelID,
		discord.NewMessageCreate().
			WithContent(response).
			WithMessageReferenceByID(event.MessageID),
	)
	if err != nil {
		slog.Warn("failed to send rick response", "error", err)
	}
}

func (b *Bot) callClaude(ctx context.Context, systemPrompt, contextText string) (string, error) {
	client := anthropic.NewClient(option.WithAPIKey(b.config.AnthropicAPIKey))
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     b.config.AnthropicModel,
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(contextText))},
	})
	if err != nil {
		return "", err
	}
	if len(msg.Content) == 0 {
		return rickFallback, nil
	}
	return msg.Content[0].Text, nil
}

func (b *Bot) replyFallback(event *events.MessageCreate) {
	_, err := event.Client().Rest.CreateMessage(event.ChannelID,
		discord.NewMessageCreate().
			WithContent(rickFallback).
			WithMessageReferenceByID(event.MessageID),
	)
	if err != nil {
		slog.Warn("failed to send fallback reply", "error", err)
	}
}

func (b *Bot) memberName(user discord.User) string {
	name, err := b.GetUsernameForID(user.ID.String())
	if err != nil {
		return user.Username
	}
	return name
}
