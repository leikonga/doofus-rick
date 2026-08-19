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
	normalMaxIter = 8
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
	ctx, cancel := context.WithTimeout(ctx, a.turnTimeout)
	defer cancel()

	// Typing starts immediately, in parallel with history/roster/recall
	// fetches below, so the indicator isn't gated behind the (sometimes
	// multi-second) embedding call recall retrieval makes.
	var theatreDone <-chan struct{}
	if _, alreadyTyping := a.typingChannels.LoadOrStore(event.ChannelID, struct{}{}); !alreadyTyping {
		if seq := a.typingTheatre.GetTypingSequence(); len(seq) > 0 {
			theatreDone = a.runTypingTheatre(ctx, event, seq)
			go func() {
				defer a.typingChannels.Delete(event.ChannelID)
				<-theatreDone
				a.keepTyping(ctx, event)
			}()
		} else {
			go func() {
				defer a.typingChannels.Delete(event.ChannelID)
				a.keepTyping(ctx, event)
			}()
		}
	}

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

	messages := buildTranscript(botID, event.MessageID, msgs, a.memberName)

	if ref := event.Message.MessageReference; ref != nil && ref.Type == discord.MessageReferenceTypeDefault && ref.MessageID != nil {
		refMsg, err := event.Client().Rest.GetMessage(event.ChannelID, *ref.MessageID)
		if err == nil && !refMsg.Author.Bot && len(refMsg.Content) <= maxContextLen {
			ts := refMsg.CreatedAt.Format("15:04")
			messages = append(messages, llm.NewUserMessage(llm.TextPart(fmt.Sprintf("[%s %s (replied to)]: %s", ts, a.memberName(refMsg.Author), a.resolveMentions(refMsg.Content)))))
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
	var triggerLabel string
	if len(triggerParts) > 0 {
		triggerLabel = fmt.Sprintf("[%s]: %s", triggerName, strings.Join(triggerParts, " "))
	} else {
		triggerLabel = fmt.Sprintf("[%s]: (pinged Rick)", triggerName)
	}

	triggerContentParts := []llm.ContentPart{llm.TextPart(triggerLabel)}
	for _, url := range attachments.imageURLs {
		triggerContentParts = append(triggerContentParts, llm.ImagePart(url))
	}
	triggerContentParts = append(triggerContentParts, attachments.fileParts...)
	if len(messages) > 0 {
		messages = append(messages, checkpointMessage())
	}
	messages = append(messages, llm.NewUserMessage(triggerContentParts...))

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

	recallCh := make(chan string, 1)
	go func() {
		recallCh <- a.buildRecallBlock(ctx, triggerContent, a.visibleChannelIDs(event.Message.Author.ID))
	}()

	leit, gradDo := a.buildUserRoster(ctx, channelOverwrites)

	recall := <-recallCh

	resp, err := a.callModel(ctx, modelRequest{
		systemPrompt: string(systemPrompt),
		cachedPrefix: buildCachedPrefix(leit, channelName, channelTopic),
		uncachedTail: buildUncachedTail(gradDo, recall),
		messages:     messages,
		tracePrompt:  triggerLabel,
		event:        event,
	})
	if err != nil {
		slog.Warn("model call failed", "error", err)
		return
	}

	if theatreDone != nil {
		select {
		case <-theatreDone:
		case <-ctx.Done():
		}
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

// checkpointMessage breaks the run of consecutive user turns, since
// Anthropic silently merges those and would fold the trigger into history.
func checkpointMessage() llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.TextPart("kontext gelesen")}}
}

func buildTranscript(botID, skipID snowflake.ID, msgs []discord.Message, memberNameFunc func(discord.User) string) []llm.Message {
	var messages []llm.Message
	for _, msg := range msgs {
		if msg.ID == skipID || msg.Author.ID == botID || strings.HasPrefix(msg.Content, "/") {
			continue
		}

		var parts []llm.ContentPart
		content := msg.Content
		if len(content) > maxContextLen {
			content = content[:maxContextLen] + "..."
		}
		if content != "" {
			parts = append(parts, llm.TextPart(content))
		}
		for _, s := range msg.StickerItems {
			parts = append(parts, llm.TextPart("(sticker: "+s.Name+")"))
		}
		for _, att := range msg.Attachments {
			if isImageAttachment(att) {
				parts = append(parts, llm.TextPart("(sent an image)"))
			} else {
				parts = append(parts, llm.TextPart(unsupportedLabel(att)))
			}
		}
		if len(parts) == 0 {
			continue
		}

		name := memberNameFunc(msg.Author)
		if msg.Author.Bot {
			name = msg.Author.Username + " (bot)"
		}
		ts := msg.CreatedAt.Format("15:04")
		contentText := fmt.Sprintf("[%s %s]: %s", ts, name, strings.Join(partsText(parts), " "))

		messages = append(messages, llm.NewUserMessage(llm.TextPart(contentText)))
	}
	return messages
}

func partsText(parts []llm.ContentPart) []string {
	var texts []string
	for _, p := range parts {
		if p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return texts
}

type modelRequest struct {
	systemPrompt string
	cachedPrefix string
	uncachedTail string
	messages     []llm.Message
	tracePrompt  string
	event        *events.MessageCreate
}

func (a *Agent) callModel(ctx context.Context, req modelRequest) (retResp llm.RickResponse, retErr error) {
	model := a.config.RickModel
	// Differs from model when a fallback fires; usage bills against the model that ran.
	servedModel := model

	rec := a.tracer.Start(req.event.ChannelID.String(), req.event.Message.Author.ID.String(), req.systemPrompt+req.cachedPrefix, req.tracePrompt)
	defer func() {
		resp, err := retResp, retErr
		go func() {
			e := rec.Finish(resp.Text, resp.Decline, err)
			if e.InputTokens > 0 || e.OutputTokens > 0 {
				saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				a.store.SaveTokenUsage(saveCtx, e.ChannelID, e.UserID, servedModel, e.InputTokens, e.OutputTokens)
			}
		}()
	}()

	tools := a.buildTools(req.event)

	messages := req.messages

	var pendingText string
	var escalated bool
	maxIter := normalMaxIter
	// maxIter is re-read each iteration, so escalation mid-turn extends the loop; do not convert to `for range`.
	for iter := 0; iter < maxIter; iter++ {
		if msgsJSON, err := json.Marshal(messages); err == nil {
			rec.SetMessages(msgsJSON)
		}

		systemFull := req.systemPrompt + req.cachedPrefix
		if req.uncachedTail != "" {
			systemFull += "\n\n" + req.uncachedTail
		}

		maxTokens := a.config.RickMaxTokens
		if escalated {
			maxTokens = a.config.CodeMaxTokens
		}

		resp, err := a.llm.Complete(ctx, llm.CompletionRequest{
			Model:          model,
			FallbackModels: a.config.RickFallbackModels,
			MaxTokens:      maxTokens,
			SessionID:      req.event.ChannelID.String(),
			System:         systemFull,
			Messages:       messages,
			Tools:          tools,
		})
		if err != nil {
			slog.Warn("model api error", "error", err)
			return llm.RickResponse{}, err
		}
		if resp.Model != "" {
			servedModel = resp.Model
		}
		rec.AddTokens(resp.InputTokens, resp.OutputTokens)

		// Some providers OpenRouter proxies report finish_reason "stop" even
		// though tool_calls is populated, so check the array directly rather
		// than trusting StopReason.
		if len(resp.Message.ToolCalls) == 0 {
			text := messageText(resp.Message)
			if text != "" {
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

		if escalateForCode(escalated, resp.Message.ToolCalls) {
			escalated = true
			maxIter = a.config.CodeMaxToolIter
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
	slog.Warn("tool iteration limit reached, declining", "max_iter", maxIter)
	return llm.RickResponse{Decline: true}, nil
}

func escalateForCode(alreadyEscalated bool, calls []llm.ToolCall) bool {
	if alreadyEscalated {
		return true
	}
	for _, call := range calls {
		if strings.HasPrefix(call.Name, "code_") {
			return true
		}
	}
	return false
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

// runTypingTheatre plays a scripted [type, silent, type] sequence instead of
// a continuous typing indicator, and returns a channel closed once it's
// done, so the caller can hold the response back until the sequence plays
// out rather than sending as soon as the model responds.
func (a *Agent) runTypingTheatre(ctx context.Context, event *events.MessageCreate, sequence []time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i, d := range sequence {
			if i%2 == 0 {
				if err := event.Client().Rest.SendTyping(event.ChannelID); err != nil {
					slog.Warn("failed to send typing indicator", "error", err)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
		}
	}()
	return done
}

func (a *Agent) memberName(user discord.User) string {
	name, err := a.discord.GetUsernameForID(user.ID.String())
	if err != nil || name == "" {
		return user.Username
	}
	return name
}

func (a *Agent) resolveMentions(content string) string {
	return userMentionRe.ReplaceAllStringFunc(content, func(match string) string {
		id := userMentionRe.FindStringSubmatch(match)[1]
		name, err := a.discord.GetUsernameForID(id)
		if err != nil || name == "" {
			return "@unknown-user"
		}
		return "@" + name
	})
}

func buildCachedPrefix(roster, channelName, channelTopic string) string {
	var sb strings.Builder
	if roster != "" {
		sb.WriteString(roster)
	}
	if channelName != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "# channel: %s", channelName)
		if channelTopic != "" {
			fmt.Fprintf(&sb, "\n# topic: %s", channelTopic)
		}
	}
	return sb.String()
}

func buildUncachedTail(gradDo, recall string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<now>%s</now>", time.Now().Format("2006-01-02 15:04 MST"))
	if gradDo != "" {
		sb.WriteString("\n\n")
		sb.WriteString(gradDo)
	}
	if recall != "" {
		sb.WriteString("\n\n")
		sb.WriteString(recall)
	}
	return sb.String()
}

// buildRecallBlock runs the hybrid retrieval pre-fetch for the triggering
// message and renders it for the uncached tail, or "" if recall is off, the
// query is empty, no channels are visible, or nothing clears
// RECALL_MIN_SCORE. Never blocks the persona call on failure.
func (a *Agent) buildRecallBlock(ctx context.Context, query string, channelIDs []uint64) string {
	if !a.config.RecallEnabled || a.retriever == nil || query == "" || len(channelIDs) == 0 {
		return ""
	}
	chunks, err := a.retriever.Retrieve(ctx, query, channelIDs)
	if err != nil {
		slog.Warn("recall retrieval failed", "error", err)
		return ""
	}
	return a.retriever.BuildRecallBlock(chunks)
}

// visibleChannelIDs lists the guild message channels the given member can
// see, so recall retrieval isn't scoped to just the channel a mention
// happened to land in and doesn't leak content from channels the asking
// user can't access.
func (a *Agent) visibleChannelIDs(requesterID snowflake.ID) []uint64 {
	guildID, err := snowflake.Parse(a.config.DiscordGuild)
	if err != nil {
		return nil
	}
	channels, err := a.discordClient.Rest.GetGuildChannels(guildID)
	if err != nil {
		slog.Warn("failed to list guild channels for recall scope", "error", err)
		return nil
	}
	member, err := a.discord.GetMemberForID(requesterID.String())
	if err != nil || member == nil {
		return nil
	}

	var ids []uint64
	for _, ch := range channels {
		gmc, ok := ch.(discord.GuildMessageChannel)
		if !ok {
			continue
		}
		if memberCanSeeChannel(*member, gmc.PermissionOverwrites()) {
			ids = append(ids, uint64(ch.ID()))
		}
	}
	return ids
}
