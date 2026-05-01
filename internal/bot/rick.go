package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

const (
	maxContextLen = 500
	maxTokens     = int64(512)
	historyLimit  = 11
	maxImages     = 5
	rickFallback  = "najo woas i etz ned"
)

type rickResponse struct {
	text    string
	decline bool
	emoji   string
}

func (b *Bot) onMentionCreate(event *events.MessageCreate) {
	if event.Message.Author.Bot {
		return
	}

	botID := event.Client().ID()
	if !slices.ContainsFunc(event.Message.Mentions, func(u discord.User) bool { return u.ID == botID }) {
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
		if msg.Author.Bot && msg.Author.ID != botID {
			continue
		}
		if strings.HasPrefix(msg.Content, "/") {
			continue
		}
		if len(msg.Content) > maxContextLen {
			continue
		}
		name := "Rick"
		if !msg.Author.Bot {
			name = b.memberName(msg.Author)
		}
		lines = append(lines, fmt.Sprintf("[%s]: %s", name, msg.Content))
	}

	if ref := event.Message.MessageReference; ref != nil && ref.Type == discord.MessageReferenceTypeDefault && ref.MessageID != nil {
		refMsg, err := event.Client().Rest.GetMessage(event.ChannelID, *ref.MessageID)
		if err == nil && !refMsg.Author.Bot && len(refMsg.Content) <= maxContextLen {
			lines = append([]string{fmt.Sprintf("[%s (replied to)]: %s", b.memberName(refMsg.Author), refMsg.Content)}, lines...)
		}
	}

	triggerContent := strings.TrimSpace(
		strings.NewReplacer(
			fmt.Sprintf("<@%s>", botID), "",
			fmt.Sprintf("<@!%s>", botID), "",
		).Replace(event.Message.Content),
	)

	var triggerParts []string
	if triggerContent != "" {
		triggerParts = append(triggerParts, triggerContent)
	}
	for _, s := range event.Message.StickerItems {
		triggerParts = append(triggerParts, "(sticker: "+s.Name+")")
	}

	var trigger string
	if len(triggerParts) > 0 {
		trigger = fmt.Sprintf("[%s]: %s", b.memberName(event.Message.Author), strings.Join(triggerParts, " "))
	}

	var imageURLs []string
	for _, att := range event.Message.Attachments {
		if att.ContentType != nil && strings.HasPrefix(*att.ContentType, "image/") {
			imageURLs = append(imageURLs, att.URL)
			if len(imageURLs) >= maxImages {
				break
			}
		}
	}

	var channelName, channelTopic string
	if ch, err := event.Client().Rest.GetChannel(event.ChannelID); err == nil {
		channelName = ch.Name()
		if gmc, ok := ch.(discord.GuildMessageChannel); ok && gmc.Topic() != nil {
			channelTopic = *gmc.Topic()
		}
	}

	fullSystem := string(systemPrompt)
	if roster := b.buildUserRoster(); roster != "" {
		fullSystem += "\n\n" + roster
	}

	prompt := buildPrompt(channelName, channelTopic, lines, trigger)

	typingCtx, stopTyping := context.WithCancel(context.Background())
	defer stopTyping()
	go b.keepTyping(typingCtx, event)

	resp, err := b.callClaude(context.Background(), fullSystem, prompt, imageURLs)
	if err != nil {
		slog.Warn("claude api call failed", "error", err)
		b.replyFallback(event)
		return
	}

	if resp.decline {
		stopTyping()
		if resp.emoji != "" {
			if err := event.Client().Rest.AddReaction(event.ChannelID, event.MessageID, resp.emoji); err != nil {
				slog.Warn("failed to add reaction", "error", err)
			}
		}
		return
	}

	_, err = event.Client().Rest.CreateMessage(event.ChannelID,
		discord.NewMessageCreate().
			WithContent(resp.text).
			WithMessageReferenceByID(event.MessageID),
	)
	if err != nil {
		slog.Warn("failed to send rick response", "error", err)
	}
}

func buildPrompt(channelName, channelTopic string, lines []string, trigger string) string {
	var sb strings.Builder

	if channelTopic != "" {
		fmt.Fprintf(&sb, "<channel name=%q topic=%q />", channelName, channelTopic)
	} else if channelName != "" {
		fmt.Fprintf(&sb, "<channel name=%q />", channelName)
	}

	if len(lines) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("<history>\n")
		sb.WriteString(strings.Join(lines, "\n"))
		sb.WriteString("\n</history>")
	}

	if trigger != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("<message>\n")
		sb.WriteString(trigger)
		sb.WriteString("\n</message>")
	}

	return sb.String()
}

func (b *Bot) callClaude(ctx context.Context, systemPrompt, prompt string, imageURLs []string) (rickResponse, error) {
	blocks := []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(prompt)}
	for _, url := range imageURLs {
		blocks = append(blocks, anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: url}))
	}

	declineTool := anthropic.ToolUnionParam{
		OfTool: &anthropic.ToolParam{
			Name:        "decline",
			Description: anthropic.String("Decline to respond when you have nothing to say or genuinely don't care. Optionally react with a unicode emoji."),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"emoji": map[string]any{
						"type":        "string",
						"description": "Unicode emoji to react with. Omit to do nothing at all.",
					},
				},
			},
		},
	}

	msg, err := b.anthropicClient.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     b.config.AnthropicModel,
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
		Tools:     []anthropic.ToolUnionParam{declineTool},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(blocks...)},
	})
	if err != nil {
		return rickResponse{}, err
	}

	if msg.StopReason == anthropic.StopReasonToolUse {
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.Name == "decline" {
				var input struct {
					Emoji string `json:"emoji"`
				}
				json.Unmarshal(block.Input, &input)
				return rickResponse{decline: true, emoji: input.Emoji}, nil
			}
		}
	}

	if len(msg.Content) == 0 {
		return rickResponse{text: rickFallback}, nil
	}
	return rickResponse{text: msg.Content[0].Text}, nil
}

func (b *Bot) buildUserRoster() string {
	members := b.onlineMembers()
	if len(members) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<users>\n")
	for _, m := range members {
		fmt.Fprintf(&sb, "%s <@%s>", m.EffectiveName(), m.User.ID)
		if val, ok := b.voiceChannels.Load(m.User.ID); ok {
			if ch := val.(string); ch != "" {
				fmt.Fprintf(&sb, " (in VC: %s)", ch)
			} else {
				sb.WriteString(" (in VC)")
			}
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("</users>")
	return sb.String()
}

func (b *Bot) keepTyping(ctx context.Context, event *events.MessageCreate) {
	for {
		if err := event.Client().Rest.SendTyping(event.ChannelID); err != nil {
			slog.Warn("failed to send typing indicator", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(8 * time.Second):
		}
	}
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
