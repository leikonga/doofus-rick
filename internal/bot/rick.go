package bot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

var (
	trailingTagRe = regexp.MustCompile(`(\s*<[^>]*>\s*)+$`)
	userMentionRe = regexp.MustCompile(`<@!?(\d+)>`)
)

const (
	maxContextLen = 500
	maxTokens     = int64(512)
	historyLimit  = 7
	maxImages     = 5
	maxToolIter   = 5
	rickFallback  = "najo woas i etz ned"
)

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
		if msg.Author.ID == botID {
			continue
		}
		if strings.HasPrefix(msg.Content, "/") {
			continue
		}

		var parts []string
		content := b.resolveMentions(msg.Content)
		if len(content) > maxContextLen {
			content = content[:maxContextLen] + "..."
		}
		if content != "" {
			parts = append(parts, content)
		}
		for _, s := range msg.StickerItems {
			parts = append(parts, "(sticker: "+s.Name+")")
		}
		for _, att := range msg.Attachments {
			if att.ContentType != nil && strings.HasPrefix(*att.ContentType, "image/") {
				parts = append(parts, "(sent an image)")
			} else {
				parts = append(parts, "(sent: "+att.Filename+")")
			}
		}
		if len(parts) == 0 {
			continue
		}

		var name string
		if msg.Author.Bot {
			name = msg.Author.Username + " (bot)"
		} else {
			name = b.memberName(msg.Author)
		}
		ts := msg.CreatedAt.Format("15:04")
		lines = append(lines, fmt.Sprintf("[%s %s]: %s", ts, name, strings.Join(parts, " ")))
	}

	if ref := event.Message.MessageReference; ref != nil && ref.Type == discord.MessageReferenceTypeDefault && ref.MessageID != nil {
		refMsg, err := event.Client().Rest.GetMessage(event.ChannelID, *ref.MessageID)
		if err == nil && !refMsg.Author.Bot && len(refMsg.Content) <= maxContextLen {
			ts := refMsg.CreatedAt.Format("15:04")
			lines = append([]string{fmt.Sprintf("[%s %s (replied to)]: %s", ts, b.memberName(refMsg.Author), b.resolveMentions(refMsg.Content))}, lines...)
		}
	}

	triggerContent := strings.TrimSpace(
		strings.NewReplacer(
			fmt.Sprintf("<@%s>", botID), "",
			fmt.Sprintf("<@!%s>", botID), "",
		).Replace(event.Message.Content),
	)
	triggerContent = b.resolveMentions(triggerContent)

	var triggerParts []string
	if triggerContent != "" {
		triggerParts = append(triggerParts, triggerContent)
	}
	for _, s := range event.Message.StickerItems {
		triggerParts = append(triggerParts, "(sticker: "+s.Name+")")
	}
	for _, att := range event.Message.Attachments {
		if att.ContentType == nil || !strings.HasPrefix(*att.ContentType, "image/") {
			triggerParts = append(triggerParts, "(sent: "+att.Filename+")")
		}
	}

	triggerName := b.memberName(event.Message.Author)
	var trigger string
	if len(triggerParts) > 0 {
		trigger = fmt.Sprintf("[%s]: %s", triggerName, strings.Join(triggerParts, " "))
	} else {
		trigger = fmt.Sprintf("[%s]: (pinged Rick)", triggerName)
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
	var channelOverwrites discord.PermissionOverwrites
	if ch, err := event.Client().Rest.GetChannel(event.ChannelID); err == nil {
		channelName = ch.Name()
		if gmc, ok := ch.(discord.GuildMessageChannel); ok {
			if gmc.Topic() != nil {
				channelTopic = *gmc.Topic()
			}
			channelOverwrites = gmc.PermissionOverwrites()
		}
	}

	fullSystem := string(systemPrompt)
	fullSystem += "\n\n<now>" + time.Now().Format("2006-01-02 15:04 MST") + "</now>"
	if roster := b.buildUserRoster(channelOverwrites); roster != "" {
		fullSystem += "\n\n" + roster
	}

	prompt := buildPrompt(channelName, channelTopic, lines, trigger)

	typingCtx, stopTyping := context.WithCancel(context.Background())
	defer stopTyping()
	go b.keepTyping(typingCtx, event)

	resp, err := b.callClaude(context.Background(), fullSystem, prompt, imageURLs, event)
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

	sanitizedResponse := strings.TrimSpace(trailingTagRe.ReplaceAllString(resp.text, ""))
	msg := discord.NewMessageCreate().WithMessageReferenceByID(event.MessageID)
	if sanitizedResponse != "" {
		msg = msg.WithContent(sanitizedResponse)
	}
	if resp.embed != nil {
		msg = msg.WithEmbeds(*resp.embed)
	}
	if sanitizedResponse != "" || resp.embed != nil {
		if _, err = event.Client().Rest.CreateMessage(event.ChannelID, msg); err != nil {
			slog.Warn("failed to send rick response", "error", err)
		}
	}
}

func buildPrompt(channelName, channelTopic string, lines []string, trigger string) string {
	var sb strings.Builder

	if channelName != "" {
		fmt.Fprintf(&sb, "# channel: %s", channelName)
		if channelTopic != "" {
			fmt.Fprintf(&sb, "\n# topic: %s", channelTopic)
		}
	}

	if len(lines) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("[context - recent chat, do not respond to these]\n")
		sb.WriteString(strings.Join(lines, "\n"))
	}

	if trigger != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("[reply to this mention]\n")
		sb.WriteString(trigger)
	}

	return sb.String()
}

func (b *Bot) callClaude(ctx context.Context, systemPrompt, prompt string, imageURLs []string, event *events.MessageCreate) (rickResponse, error) {
	allTools := b.buildTools(event)

	defs := make([]anthropic.ToolUnionParam, len(allTools))
	lookup := make(map[string]ricktool, len(allTools))
	for i, t := range allTools {
		defs[i] = t.def
		lookup[t.name] = t
	}

	blocks := []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(prompt)}
	for _, url := range imageURLs {
		blocks = append(blocks, anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: url}))
	}

	messages := []anthropic.MessageParam{anthropic.NewUserMessage(blocks...)}

	var pendingText string
	for range maxToolIter {
		msg, err := b.anthropicClient.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     b.config.AnthropicModel,
			MaxTokens: maxTokens,
			System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
			Tools:     defs,
			Messages:  messages,
		})
		if err != nil {
			return rickResponse{}, err
		}

		if msg.StopReason != anthropic.StopReasonToolUse {
			for _, block := range msg.Content {
				if block.Type == "text" && block.Text != "" {
					return rickResponse{text: block.Text}, nil
				}
			}
			if pendingText != "" {
				return rickResponse{text: pendingText}, nil
			}
			return rickResponse{text: rickFallback}, nil
		}

		messages = append(messages, msg.ToParam())

		var resultBlocks []anthropic.ContentBlockParamUnion
		for _, block := range msg.Content {
			if block.Type == "text" && block.Text != "" {
				pendingText = block.Text
				continue
			}
			if block.Type != "tool_use" {
				continue
			}
			tool, ok := lookup[block.Name]
			if !ok {
				slog.Warn("unknown tool called by claude", "tool", block.Name)
				continue
			}
			slog.Info("tool call", "tool", block.Name, "input", string(block.Input))
			result, err := tool.execute(ctx, block.Input)
			if err != nil {
				slog.Warn("tool execution failed", "tool", block.Name, "error", err)
				resultBlocks = append(resultBlocks, anthropic.NewToolResultBlock(block.ID, err.Error(), true))
				continue
			}
			if result.response != nil {
				return *result.response, nil
			}
			resultBlocks = append(resultBlocks, anthropic.NewToolResultBlock(block.ID, result.content, false))
		}

		if len(resultBlocks) == 0 {
			break
		}
		messages = append(messages, anthropic.NewUserMessage(resultBlocks...))
	}

	return rickResponse{text: rickFallback}, nil
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

func (b *Bot) resolveMentions(content string) string {
	return userMentionRe.ReplaceAllStringFunc(content, func(match string) string {
		id := userMentionRe.FindStringSubmatch(match)[1]
		name, err := b.GetUsernameForID(id)
		if err != nil {
			return "@unknown-user"
		}
		return "@" + name
	})
}
