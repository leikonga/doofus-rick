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
	historyLimit  = 7
	maxImages     = 5
	maxToolIter   = 5
	rickFallback  = "najo woas i etz ned"
)

type rickResponse struct {
	text    string
	decline bool
	emoji   string
}

type toolResult struct {
	response *rickResponse
	content  string
}

type ricktool struct {
	name    string
	def     anthropic.ToolUnionParam
	execute func(ctx context.Context, input json.RawMessage) (toolResult, error)
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
		if msg.Author.ID == botID {
			continue
		}
		if strings.HasPrefix(msg.Content, "/") {
			continue
		}

		var parts []string
		content := msg.Content
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
			lines = append([]string{fmt.Sprintf("[%s %s (replied to)]: %s", ts, b.memberName(refMsg.Author), refMsg.Content)}, lines...)
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
	if roster := b.buildUserRoster(channelOverwrites); roster != "" {
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

	sanitizedResponse := strings.TrimRight(resp.text, "\n<br>")
	_, err = event.Client().Rest.CreateMessage(event.ChannelID,
		discord.NewMessageCreate().
			WithContent(sanitizedResponse).
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

func (b *Bot) buildTools() []ricktool {
	return []ricktool{
		{
			name: "decline",
			def: anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name:        "decline",
					Description: anthropic.String("Decline to respond, but only for comedic effect. Use sparingly. Prefer responding even if you have little to say."),
					InputSchema: anthropic.ToolInputSchemaParam{
						Properties: map[string]any{
							"emoji": map[string]any{
								"type":        "string",
								"description": "Unicode emoji to react with. Omit to do nothing at all.",
							},
						},
					},
				},
			},
			execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
				var in struct {
					Emoji string `json:"emoji"`
				}
				if err := json.Unmarshal(input, &in); err != nil {
					return toolResult{}, err
				}
				return toolResult{response: &rickResponse{decline: true, emoji: in.Emoji}}, nil
			},
		},
		{
			name: "gif_search",
			def: anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name:        "gif_search",
					Description: anthropic.String("Post a GIF as a response. Use sparingly, only for comedic effect. Prefer responding with text."),
					InputSchema: anthropic.ToolInputSchemaParam{
						Properties: map[string]any{
							"query": map[string]any{
								"type":        "string",
								"description": "Search term for the GIF.",
							},
							"caption": map[string]any{
								"type":        "string",
								"description": "Optional short text to post alongside the GIF.",
							},
						},
						Required: []string{"query"},
					},
				},
			},
			execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
				var in struct {
					Query   string `json:"query"`
					Caption string `json:"caption"`
				}
				if err := json.Unmarshal(input, &in); err != nil {
					return toolResult{}, err
				}
				gifURL, err := b.searchGiphy(ctx, in.Query)
				if err != nil {
					return toolResult{}, err
				}
				text := gifURL
				if in.Caption != "" {
					text = in.Caption + "\n" + gifURL
				}
				return toolResult{response: &rickResponse{text: text}}, nil
			},
		},
		{
			name: "web_search",
			def: anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name: "web_search",
					Description: anthropic.String("Search the web and get extracted content from the top results. " +
						"Use the site: operator to target specific sources, e.g. " +
						"site:knowyourmeme.com to look up meme origins and status, " +
						"site:reddit.com for community takes, " +
						"site:youtube.com to find a specific video. " +
						"Prefer a targeted query over a vague one."),
					InputSchema: anthropic.ToolInputSchemaParam{
						Properties: map[string]any{
							"query": map[string]any{
								"type":        "string",
								"description": "Search query. Supports standard operators like site:, intitle:, etc.",
							},
						},
						Required: []string{"query"},
					},
				},
			},
			execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
				var in struct {
					Query string `json:"query"`
				}
				if err := json.Unmarshal(input, &in); err != nil {
					return toolResult{}, err
				}
				result, err := b.searchBrave(ctx, in.Query)
				if err != nil {
					return toolResult{}, err
				}
				return toolResult{content: result}, nil
			},
		},
		{
			name: "remember",
			def: anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name:        "remember",
					Description: anthropic.String("Save something to persistent memory for future reference. Use for notable facts about users, running jokes, or anything worth recalling later."),
					InputSchema: anthropic.ToolInputSchemaParam{
						Properties: map[string]any{
							"content": map[string]any{
								"type":        "string",
								"description": "The thing to remember.",
							},
							"user_id": map[string]any{
								"type":        "string",
								"description": "Discord user ID this memory is about. Omit for general memories.",
							},
							"tags": map[string]any{
								"type":        "array",
								"items":       map[string]any{"type": "string"},
								"description": "Optional tags to aid recall.",
							},
						},
						Required: []string{"content"},
					},
				},
			},
			execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
				var in struct {
					Content string   `json:"content"`
					UserID  string   `json:"user_id"`
					Tags    []string `json:"tags"`
				}
				if err := json.Unmarshal(input, &in); err != nil {
					return toolResult{}, err
				}
				if err := b.store.SaveMemory(in.UserID, in.Content, in.Tags); err != nil {
					return toolResult{}, err
				}
				return toolResult{content: "remembered"}, nil
			},
		},
		{
			name: "recall",
			def: anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name:        "recall",
					Description: anthropic.String("Search persistent memory for relevant facts. Use when someone says something that might connect to past events, or when you want to land a callback."),
					InputSchema: anthropic.ToolInputSchemaParam{
						Properties: map[string]any{
							"query": map[string]any{
								"type":        "string",
								"description": "Search term to match against stored memories.",
							},
							"user_id": map[string]any{
								"type":        "string",
								"description": "Filter to memories about this Discord user ID.",
							},
						},
						Required: []string{"query"},
					},
				},
			},
			execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
				var in struct {
					Query  string `json:"query"`
					UserID string `json:"user_id"`
				}
				if err := json.Unmarshal(input, &in); err != nil {
					return toolResult{}, err
				}
				memories, err := b.store.SearchMemory(in.Query, in.UserID)
				if err != nil {
					return toolResult{}, err
				}
				if len(memories) == 0 {
					return toolResult{content: "no memories found"}, nil
				}
				var sb strings.Builder
				for _, m := range memories {
					fmt.Fprintf(&sb, "- %s\n", m.Content)
				}
				return toolResult{content: sb.String()}, nil
			},
		},
		{
			name: "shell_exec",
			def: anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name: "shell_exec",
					Description: anthropic.String("Run a shell command on your own server and get the output. " +
						"You are running inside an Alpine Linux container as an unprivileged user, " +
						"so standard busybox utilities are available (sh, ls, ps, df, free, uptime, uname, cat, date, wget etc.) " +
						"but you cannot escalate privileges or access host resources. " +
						"Use for comedic self-awareness about your own environment."),
					InputSchema: anthropic.ToolInputSchemaParam{
						Properties: map[string]any{
							"command": map[string]any{
								"type":        "string",
								"description": "Shell command to run.",
							},
						},
						Required: []string{"command"},
					},
				},
			},
			execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
				var in struct {
					Command string `json:"command"`
				}
				if err := json.Unmarshal(input, &in); err != nil {
					return toolResult{}, err
				}
				return toolResult{content: b.shellExec(ctx, in.Command)}, nil
			},
		},
	}
}

func (b *Bot) callClaude(ctx context.Context, systemPrompt, prompt string, imageURLs []string) (rickResponse, error) {
	allTools := b.buildTools()

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
			if len(msg.Content) == 0 {
				return rickResponse{text: rickFallback}, nil
			}
			return rickResponse{text: msg.Content[0].Text}, nil
		}

		messages = append(messages, msg.ToParam())

		var resultBlocks []anthropic.ContentBlockParamUnion
		for _, block := range msg.Content {
			if block.Type != "tool_use" {
				continue
			}
			tool, ok := lookup[block.Name]
			if !ok {
				slog.Warn("unknown tool called by claude", "tool", block.Name)
				continue
			}
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

func (b *Bot) buildUserRoster(overwrites discord.PermissionOverwrites) string {
	members := b.onlineMembers()
	if len(members) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<users>\n")
	for _, m := range members {
		if !memberCanSeeChannel(m, overwrites) {
			continue
		}
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

// memberCanSeeChannel checks channel permission overwrites to determine visibility.
// It does not account for base role permissions (guild-level), which requires
// fetching the full guild. For channels with no restrictive overwrites (the common
// case), all members pass through, which is correct.
func memberCanSeeChannel(member discord.Member, overwrites discord.PermissionOverwrites) bool {
	if len(overwrites) == 0 {
		return true
	}

	// Start from @everyone overwrite (role ID == guild ID, but we check all role overwrites)
	allow, deny := discord.PermissionsNone, discord.PermissionsNone

	// Accumulate role overwrites
	for _, roleID := range member.RoleIDs {
		if ow, ok := overwrites.Role(roleID); ok {
			deny = deny.Add(ow.Deny)
			allow = allow.Add(ow.Allow)
		}
	}

	// Member-specific overwrite takes highest priority
	if ow, ok := overwrites.Member(member.User.ID); ok {
		deny = deny.Add(ow.Deny)
		allow = allow.Add(ow.Allow)
	}

	if deny.Has(discord.PermissionViewChannel) && !allow.Has(discord.PermissionViewChannel) {
		return false
	}
	return true
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
