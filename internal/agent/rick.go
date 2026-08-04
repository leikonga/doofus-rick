package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
)

var (
	trailingTagRe = regexp.MustCompile(`(\s*<[^>]*>\s*)+$`)
	userMentionRe = regexp.MustCompile(`<@!?(\d+)>`)
)

const (
	maxContextLen = 500
	historyLimit  = 7
	maxToolIter   = 8
)

func (a *Agent) HandleMention(ctx context.Context, event *events.MessageCreate) {
	if event.Message.Author.Bot {
		return
	}

	botID := event.Client().ID()
	if !slices.ContainsFunc(event.Message.Mentions, func(u discord.User) bool { return u.ID == botID }) {
		return
	}

	go a.handleMention(ctx, event)
}

func (a *Agent) handleMention(ctx context.Context, event *events.MessageCreate) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	systemPrompt, err := os.ReadFile(a.config.SystemPromptFile)
	if err != nil {
		slog.Warn("failed to read system prompt file", "error", err, "path", a.config.SystemPromptFile)
		return
	}

	botID := event.Client().ID()
	msgs, err := event.Client().Rest.GetMessages(event.ChannelID, 0, 0, 0, historyLimit)
	if err != nil {
		slog.Warn("failed to fetch channel history", "error", err)
		return
	}
	slices.Reverse(msgs)

	lines := a.buildHistoryLines(botID, event.MessageID, msgs)

	if ref := event.Message.MessageReference; ref != nil && ref.Type == discord.MessageReferenceTypeDefault && ref.MessageID != nil {
		refMsg, err := event.Client().Rest.GetMessage(event.ChannelID, *ref.MessageID)
		if err == nil && !refMsg.Author.Bot && len(refMsg.Content) <= maxContextLen {
			ts := refMsg.CreatedAt.Format("15:04")
			lines = append([]string{fmt.Sprintf("[%s %s (replied to)]: %s", ts, a.memberName(refMsg.Author), a.resolveMentions(refMsg.Content))}, lines...)
		}
	}

	triggerContent := strings.TrimSpace(
		strings.NewReplacer(
			fmt.Sprintf("<@%s>", botID), "",
			fmt.Sprintf("<@!%s>", botID), "",
		).Replace(event.Message.Content),
	)
	triggerContent = a.resolveMentions(triggerContent)

	var triggerParts []string
	if triggerContent != "" {
		triggerParts = append(triggerParts, triggerContent)
	}
	for _, s := range event.Message.StickerItems {
		triggerParts = append(triggerParts, "(sticker: "+s.Name+")")
	}

	attachments := classifyAttachments(ctx, event.Message.Attachments)
	triggerParts = append(triggerParts, attachments.unsupported...)

	triggerName := a.memberName(event.Message.Author)
	var trigger string
	if len(triggerParts) > 0 {
		trigger = fmt.Sprintf("[%s]: %s", triggerName, strings.Join(triggerParts, " "))
	} else {
		trigger = fmt.Sprintf("[%s]: (pinged Rick)", triggerName)
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
	if roster := a.buildUserRoster(channelOverwrites); roster != "" {
		fullSystem += "\n\n" + roster
	}

	prompt := buildPrompt(channelName, channelTopic, lines, trigger)
	if _, alreadyTyping := a.typingChannels.LoadOrStore(event.ChannelID, struct{}{}); !alreadyTyping {
		go func() {
			defer a.typingChannels.Delete(event.ChannelID)
			a.keepTyping(ctx, event)
		}()
	}

	resp, err := a.callModel(ctx, modelRequest{
		systemPrompt: fullSystem,
		prompt:       prompt,
		imageURLs:    attachments.imageURLs,
		fileParts:    attachments.fileParts,
		event:        event,
	})
	if err != nil {
		slog.Warn("model call failed", "error", err)
		return
	}

	if resp.Decline {
		if resp.Emoji != "" {
			if err := event.Client().Rest.AddReaction(event.ChannelID, event.MessageID, resp.Emoji); err != nil {
				slog.Warn("failed to add reaction", "error", err)
			}
		}
		return
	}

	sanitizedResponse := strings.TrimSpace(trailingTagRe.ReplaceAllString(resp.Text, ""))
	if sanitizedResponse == "" {
		return
	}
	msg := discord.NewMessageCreate().WithMessageReferenceByID(event.MessageID).WithContent(sanitizedResponse)
	if _, err = event.Client().Rest.CreateMessage(event.ChannelID, msg); err != nil {
		slog.Warn("failed to send rick response", "error", err)
	}
}

// buildHistoryLines renders recent channel messages, excluding the
// triggering message and the bot's own, into chat-log lines for context.
func (a *Agent) buildHistoryLines(botID, skipID snowflake.ID, msgs []discord.Message) []string {
	var lines []string
	for _, msg := range msgs {
		if msg.ID == skipID || msg.Author.ID == botID || strings.HasPrefix(msg.Content, "/") {
			continue
		}

		var parts []string
		content := a.resolveMentions(msg.Content)
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
			if isImageAttachment(att) {
				parts = append(parts, "(sent an image)")
			} else {
				parts = append(parts, unsupportedLabel(att))
			}
		}
		if len(parts) == 0 {
			continue
		}

		var name string
		if msg.Author.Bot {
			name = msg.Author.Username + " (bot)"
		} else {
			name = a.memberName(msg.Author)
		}
		ts := msg.CreatedAt.Format("15:04")
		lines = append(lines, fmt.Sprintf("[%s %s]: %s", ts, name, strings.Join(parts, " ")))
	}
	return lines
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

// modelRequest bundles the inputs to callModel: the rendered prompt plus
// whatever images and files the triggering message attached.
type modelRequest struct {
	systemPrompt string
	prompt       string
	imageURLs    []string
	fileParts    []llm.ContentPart
	event        *events.MessageCreate
}

func (a *Agent) callModel(ctx context.Context, req modelRequest) (retResp llm.RickResponse, retErr error) {
	rec := a.tracer.Start(req.event.ChannelID.String(), req.event.Message.Author.ID.String(), req.systemPrompt, req.prompt)
	defer func() {
		resp, err := retResp, retErr
		go func() {
			e := rec.Finish(resp.Text, resp.Decline, err)
			if e.InputTokens > 0 || e.OutputTokens > 0 {
				saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				a.store.SaveTokenUsage(saveCtx, e.ChannelID, e.UserID, a.config.RickModel, e.InputTokens, e.OutputTokens)
			}
		}()
	}()

	tools := a.buildTools(req.event)

	parts := []llm.ContentPart{llm.TextPart(req.prompt)}
	for _, url := range req.imageURLs {
		parts = append(parts, llm.ImagePart(url))
	}
	parts = append(parts, req.fileParts...)

	prior := a.getSession(req.event.ChannelID)
	messages := append(prior, llm.NewUserMessage(parts...))

	validated, repaired := validateConversation(messages)
	if repaired {
		if len(validated) == 0 {
			slog.Warn("conversation empty after repair, declining")
			return llm.RickResponse{Decline: true}, nil
		}
		messages = validated
	}

	var pendingText string
	for range maxToolIter {
		if msgsJSON, err := json.Marshal(messages); err == nil {
			rec.SetMessages(msgsJSON)
		}

		resp, err := a.llm.Complete(ctx, llm.CompletionRequest{
			Model:     a.config.RickModel,
			MaxTokens: a.config.RickMaxTokens,
			System:    req.systemPrompt,
			Messages:  messages,
			Tools:     tools,
		})
		if err != nil {
			slog.Warn("model api error", "error", err)
			return llm.RickResponse{}, err
		}
		rec.AddTokens(resp.InputTokens, resp.OutputTokens)

		if resp.StopReason != llm.StopToolCalls {
			text := messageText(resp.Message)
			if text != "" {
				saved := append(messages, resp.Message)
				a.putSession(req.event.ChannelID, saved)
				return llm.RickResponse{Text: text}, nil
			}
			if pendingText != "" {
				return llm.RickResponse{Text: pendingText}, nil
			}
			slog.Warn("no text in non-tool response, declining", "stop_reason", resp.StopReason)
			return llm.RickResponse{Decline: true}, nil
		}

		messages = append(messages, resp.Message)

		if text := messageText(resp.Message); text != "" {
			pendingText = text
		}

		var toolMessages []llm.Message
		var toolDone bool
		for _, call := range resp.Message.ToolCalls {
			tool, ok := tools.Find(call.Name)
			if !ok {
				slog.Warn("unknown tool called by model", "tool", call.Name)
				continue
			}
			slog.Info("tool call", "tool", call.Name, "input", call.Arguments)
			result, err := tool.Execute(ctx, json.RawMessage(call.Arguments))
			if err != nil {
				slog.Warn("tool execution failed", "tool", call.Name, "error", err)
				rec.AddTool(call.Name, call.Arguments, err.Error(), true)
				toolMessages = append(toolMessages, llm.NewToolResultMessage(call.ID, err.Error()))
				continue
			}
			if result.Response != nil {
				rec.AddTool(call.Name, call.Arguments, "(terminal)", false)
				return *result.Response, nil
			}
			rec.AddTool(call.Name, call.Arguments, result.Content, false)
			if result.Done {
				toolDone = true
			}
			toolMessages = append(toolMessages, llm.NewToolResultMessage(call.ID, result.Content))
		}

		if toolDone {
			return llm.RickResponse{Decline: true}, nil
		}
		if len(toolMessages) == 0 {
			slog.Warn("tool_calls stop reason but no actionable tool calls, declining")
			return llm.RickResponse{Decline: true}, nil
		}
		messages = append(messages, toolMessages...)
	}
	slog.Warn("tool iteration limit reached, declining", "max_iter", maxToolIter)
	return llm.RickResponse{Decline: true}, nil
}

func messageText(m llm.Message) string {
	var sb strings.Builder
	for _, p := range m.Parts {
		if p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

func (a *Agent) keepTyping(ctx context.Context, event *events.MessageCreate) {
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()
	for {
		if err := event.Client().Rest.SendTyping(event.ChannelID); err != nil {
			slog.Warn("failed to send typing indicator", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *Agent) memberName(user discord.User) string {
	name, err := a.discord.GetUsernameForID(user.ID.String())
	if err != nil {
		return user.Username
	}
	return name
}

func (a *Agent) resolveMentions(content string) string {
	return userMentionRe.ReplaceAllStringFunc(content, func(match string) string {
		id := userMentionRe.FindStringSubmatch(match)[1]
		name, err := a.discord.GetUsernameForID(id)
		if err != nil {
			return "@unknown-user"
		}
		return "@" + name
	})
}
