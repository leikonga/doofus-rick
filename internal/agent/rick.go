package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"runtime/debug"
	"runtime/pprof"
	"slices"
	"strings"
	"sync"
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
	historyLimit  = 10
	rickLabel     = "rick (du)"
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

	labels := pprof.Labels("handler", "mention", "channel", event.ChannelID.String(), "message", event.MessageID.String())
	go pprof.Do(ctx, labels, func(ctx context.Context) { a.handleMention(ctx, event) })
}

func recoverTurn(ctx context.Context, err *error) {
	v := recover()
	if v == nil {
		return
	}
	var labels []any
	pprof.ForLabels(ctx, func(key, value string) bool {
		labels = append(labels, key, value)
		return true
	})
	slog.ErrorContext(ctx, "turn panicked", "panic", v, "stack", string(debug.Stack()), slog.Group("labels", labels...))
	if err != nil {
		*err = fmt.Errorf("turn panicked: %v", v)
	}
}

func (a *Agent) handleMention(ctx context.Context, event *events.MessageCreate) {
	defer recoverTurn(ctx, nil)

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

	history := buildHistory(botID, event.MessageID, msgs, a.memberName)

	var replyTo string
	if ref := event.Message.MessageReference; ref != nil && ref.Type == discord.MessageReferenceTypeDefault && ref.MessageID != nil {
		if refMsg, err := event.Client().Rest.GetMessage(event.ChannelID, *ref.MessageID); err == nil {
			replyTo = fmt.Sprintf(", antwortet auf %s %s: %q", authorLabel(botID, refMsg.Author, a.memberName),
				refMsg.CreatedAt.Format("15:04"), truncate(a.resolveMentions(refMsg.Content)))
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

	triggerText := "(pinged Rick)"
	if len(triggerParts) > 0 {
		triggerText = strings.Join(triggerParts, " ")
	}
	triggerLabel := fmt.Sprintf("[%s %s%s]: %s", event.Message.CreatedAt.Format("15:04"),
		a.memberName(event.Message.Author), replyTo, triggerText)

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

	now := time.Now()
	turnParts := []llm.ContentPart{llm.TextPart(buildVolatileTurn(now, a.vitals(now), gradDo, recall, history, triggerLabel))}
	for _, url := range attachments.imageURLs {
		turnParts = append(turnParts, llm.ImagePart(url))
	}
	turnParts = append(turnParts, attachments.fileParts...)

	resp, err := a.callModel(ctx, modelRequest{
		system:      string(systemPrompt) + buildCachedPrefix(a.selbstBlock, leit, channelName, channelTopic),
		messages:    []llm.Message{llm.NewUserMessage(turnParts...)},
		tracePrompt: triggerLabel,
		event:       event,
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

func buildHistory(botID, skipID snowflake.ID, msgs []discord.Message, memberNameFunc func(discord.User) string) []string {
	var lines []string
	for _, msg := range msgs {
		if msg.ID == skipID || strings.HasPrefix(msg.Content, "/") {
			continue
		}

		var parts []string
		if msg.Content != "" {
			parts = append(parts, truncate(msg.Content))
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

		lines = append(lines, fmt.Sprintf("[%s %s]: %s", msg.CreatedAt.Format("15:04"),
			authorLabel(botID, msg.Author, memberNameFunc), strings.Join(parts, " ")))
	}
	return lines
}

func authorLabel(botID snowflake.ID, author discord.User, memberNameFunc func(discord.User) string) string {
	switch {
	case author.ID == botID:
		return rickLabel
	case author.Bot:
		return author.Username + " (bot)"
	default:
		return memberNameFunc(author)
	}
}

func truncate(content string) string {
	if len(content) > maxContextLen {
		return content[:maxContextLen] + "..."
	}
	return content
}

func buildVolatileTurn(now time.Time, vitals, gradDo, recall string, history []string, trigger string) string {
	var sb strings.Builder
	sb.WriteString("<kontext>\n")
	fmt.Fprintf(&sb, "<now>%s</now>\n", now.Format("2006-01-02 15:04 MST"))
	if vitals != "" {
		sb.WriteString(vitals + "\n")
	}
	if gradDo != "" {
		sb.WriteString(strings.TrimRight(gradDo, "\n") + "\n")
	}
	if recall != "" {
		sb.WriteString(strings.TrimRight(recall, "\n") + "\n")
	}
	if len(history) > 0 {
		sb.WriteString("<verlauf>\n")
		for _, line := range history {
			sb.WriteString(line + "\n")
		}
		sb.WriteString("</verlauf>\n")
	}
	sb.WriteString("</kontext>\n<nachricht>\n")
	sb.WriteString(trigger)
	sb.WriteString("\n</nachricht>")
	return sb.String()
}

type modelRequest struct {
	system      string
	messages    []llm.Message
	tracePrompt string
	event       *events.MessageCreate
}

func (a *Agent) callModel(ctx context.Context, req modelRequest) (retResp llm.RickResponse, retErr error) {
	model := a.config.RickModel
	// Differs from model when a fallback fires; usage bills against the model that ran.
	servedModel := model

	rec := a.tracer.Start(req.event.ChannelID.String(), req.event.Message.Author.ID.String(), req.system, req.tracePrompt)
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

		maxTokens, effort := a.config.RickMaxTokens, a.config.RickReasoningEffort
		if escalated {
			maxTokens, effort = a.config.CodeMaxTokens, a.config.CodeReasoningEffort
		}

		resp, err := a.llm.Complete(ctx, llm.CompletionRequest{
			Model:           model,
			FallbackModels:  a.config.RickFallbackModels,
			MaxTokens:       maxTokens,
			ReasoningEffort: effort,
			SessionID:       req.event.ChannelID.String(),
			System:          req.system,
			Messages:        messages,
			Tools:           tools,
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

		outcomes := runToolCalls(ctx, tools, resp.Message.ToolCalls)
		terminal, toolMessages, toolDone := collectToolResults(rec, tools, resp.Message.ToolCalls, outcomes)
		if terminal != nil {
			return *terminal, nil
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

type toolOutcome struct {
	found  bool
	result llm.Result
	err    error
}

type toolRecorder interface {
	AddTool(name, input, result string, isErr bool)
}

func runToolCalls(ctx context.Context, tools llm.Tools, calls []llm.ToolCall) []toolOutcome {
	outcomes := make([]toolOutcome, len(calls))
	var codeLane []func()
	var wg sync.WaitGroup
	for i, call := range calls {
		tool, ok := tools.Find(call.Name)
		if !ok {
			continue
		}
		outcomes[i].found = true
		run := func() { outcomes[i].result, outcomes[i].err = executeTool(ctx, tool, call) }
		// code_ tools share one repo checkout, so they run one at a time in call order.
		if strings.HasPrefix(tool.Name, "code_") {
			codeLane = append(codeLane, run)
			continue
		}
		wg.Go(run)
	}
	if len(codeLane) > 0 {
		wg.Go(func() {
			for _, run := range codeLane {
				run()
			}
		})
	}
	wg.Wait()
	return outcomes
}

func executeTool(ctx context.Context, tool llm.Tool, call llm.ToolCall) (result llm.Result, err error) {
	slog.Info("tool call", "tool", call.Name, "input", call.Arguments)
	pprof.Do(ctx, pprof.Labels("tool", tool.Name), func(ctx context.Context) {
		defer func() {
			if v := recover(); v != nil {
				slog.ErrorContext(ctx, "tool panicked", "tool", tool.Name, "panic", v, "stack", string(debug.Stack()))
				err = fmt.Errorf("tool %s panicked: %v", tool.Name, v)
			}
		}()
		result, err = tool.Execute(ctx, json.RawMessage(call.Arguments))
	})
	return result, err
}

func collectToolResults(rec toolRecorder, tools llm.Tools, calls []llm.ToolCall, outcomes []toolOutcome) (terminal *llm.RickResponse, toolMessages []llm.Message, done bool) {
	for i, call := range calls {
		outcome := outcomes[i]
		if !outcome.found {
			slog.Warn("unknown tool called by model", "tool", call.Name)
			msg := fmt.Sprintf("error: no tool named %q; available tools: %s", call.Name, strings.Join(tools.Names(), ", "))
			rec.AddTool(call.Name, call.Arguments, msg, true)
			toolMessages = append(toolMessages, llm.NewToolResultMessage(call.ID, msg))
			continue
		}
		if outcome.err != nil {
			slog.Warn("tool execution failed", "tool", call.Name, "error", outcome.err)
			rec.AddTool(call.Name, call.Arguments, outcome.err.Error(), true)
			toolMessages = append(toolMessages, llm.NewToolResultMessage(call.ID, outcome.err.Error()))
			continue
		}
		if outcome.result.Response != nil {
			rec.AddTool(call.Name, call.Arguments, "(terminal)", false)
			return outcome.result.Response, nil, false
		}
		rec.AddTool(call.Name, call.Arguments, outcome.result.Content, false)
		if outcome.result.Done {
			done = true
		}
		toolMessages = append(toolMessages, llm.NewToolResultMessage(call.ID, outcome.result.Content))
	}
	return nil, toolMessages, done
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

func buildCachedPrefix(selbstBlock, roster, channelName, channelTopic string) string {
	var sb strings.Builder
	for _, block := range []string{selbstBlock, roster} {
		if block == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(block)
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
