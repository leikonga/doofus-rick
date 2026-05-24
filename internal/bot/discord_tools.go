package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/store"
)

func (b *Bot) discordTools() []ricktool {
	return []ricktool{
		b.listChannelsTool(),
		b.sendMessageTool(),
		b.sendEmbedTool(),
		b.getRecentMessagesTool(),
		b.createPollTool(),
		b.getVoiceStateTool(),
		b.sendFileTool(),
		b.searchMembersTool(),
		b.getRolesTool(),
		b.getMemberRolesTool(),
		b.scheduleReminderTool(),
	}
}

func (b *Bot) listChannelsTool() ricktool {
	return ricktool{
		name: "list_channels",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "list_channels",
				Description: anthropic.String("List all text channels in the server with their IDs, names, and topics."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{},
				},
			},
		},
		execute: func(_ context.Context, _ json.RawMessage) (toolResult, error) {
			guildID, err := snowflake.Parse(b.config.DiscordGuild)
			if err != nil {
				return toolResult{}, err
			}
			channels, err := b.client.Rest.GetGuildChannels(guildID)
			if err != nil {
				return toolResult{}, err
			}
			var sb strings.Builder
			for _, ch := range channels {
				t := ch.Type()
				if t != discord.ChannelTypeGuildText && t != discord.ChannelTypeGuildNews && t != discord.ChannelTypeGuildForum {
					continue
				}
				topic := ""
				if gmc, ok := ch.(discord.GuildMessageChannel); ok && gmc.Topic() != nil {
					topic = ": " + *gmc.Topic()
				}
				fmt.Fprintf(&sb, "%s #%s%s\n", ch.ID(), ch.Name(), topic)
			}
			if sb.Len() == 0 {
				return toolResult{content: "no text channels found"}, nil
			}
			return toolResult{content: sb.String()}, nil
		},
	}
}

func (b *Bot) sendMessageTool() ricktool {
	return ricktool{
		name: "send_message",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "send_message",
				Description: anthropic.String("Send a message to any channel by its ID. Use list_channels to find channel IDs."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"channel_id": map[string]any{
							"type":        "string",
							"description": "Discord channel snowflake ID.",
						},
						"content": map[string]any{
							"type":        "string",
							"description": "Message content to send.",
						},
					},
					Required: []string{"channel_id", "content"},
				},
			},
		},
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				ChannelID string `json:"channel_id"`
				Content   string `json:"content"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			chID, err := snowflake.Parse(in.ChannelID)
			if err != nil {
				return toolResult{}, err
			}
			if _, err := b.client.Rest.CreateMessage(chID, discord.NewMessageCreate().WithContent(in.Content)); err != nil {
				return toolResult{}, err
			}
			return toolResult{content: "message sent"}, nil
		},
	}
}

func (b *Bot) getRecentMessagesTool() ricktool {
	return ricktool{
		name: "get_recent_messages",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "get_recent_messages",
				Description: anthropic.String("Fetch recent messages from any channel. Use to snoop on what people are talking about."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"channel_id": map[string]any{
							"type":        "string",
							"description": "Discord channel snowflake ID.",
						},
						"limit": map[string]any{
							"type":        "integer",
							"description": "Number of messages to fetch (max 20).",
						},
					},
					Required: []string{"channel_id"},
				},
			},
		},
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				ChannelID string `json:"channel_id"`
				Limit     int    `json:"limit"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			if in.Limit <= 0 || in.Limit > 20 {
				in.Limit = 10
			}
			chID, err := snowflake.Parse(in.ChannelID)
			if err != nil {
				return toolResult{}, err
			}
			msgs, err := b.client.Rest.GetMessages(chID, 0, 0, 0, in.Limit)
			if err != nil {
				return toolResult{}, err
			}
			if len(msgs) == 0 {
				return toolResult{content: "no messages found"}, nil
			}
			var sb strings.Builder
			for i := len(msgs) - 1; i >= 0; i-- {
				m := msgs[i]
				name := b.memberName(m.Author)
				ts := m.CreatedAt.Format("15:04")
				content := b.resolveMentions(m.Content)
				if len(content) > maxContextLen {
					content = content[:maxContextLen] + "..."
				}
				if content == "" {
					continue
				}
				fmt.Fprintf(&sb, "[%s %s]: %s\n", ts, name, content)
			}
			return toolResult{content: sb.String()}, nil
		},
	}
}

func (b *Bot) createPollTool() ricktool {
	return ricktool{
		name: "create_poll",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "create_poll",
				Description: anthropic.String("Post a Discord native poll in any channel."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"channel_id": map[string]any{
							"type":        "string",
							"description": "Discord channel snowflake ID.",
						},
						"question": map[string]any{
							"type":        "string",
							"description": "Poll question.",
						},
						"answers": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Poll answer options (2-10).",
						},
						"duration_hours": map[string]any{
							"type":        "integer",
							"description": "How long the poll runs in hours (default 24).",
						},
						"allow_multiselect": map[string]any{
							"type":        "boolean",
							"description": "Whether users can vote for multiple answers.",
						},
					},
					Required: []string{"channel_id", "question", "answers"},
				},
			},
		},
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				ChannelID        string   `json:"channel_id"`
				Question         string   `json:"question"`
				Answers          []string `json:"answers"`
				DurationHours    int      `json:"duration_hours"`
				AllowMultiselect bool     `json:"allow_multiselect"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			if len(in.Answers) < 2 || len(in.Answers) > 10 {
				return toolResult{content: "polls require between 2 and 10 answers"}, nil
			}
			if in.DurationHours <= 0 {
				in.DurationHours = 24
			}
			chID, err := snowflake.Parse(in.ChannelID)
			if err != nil {
				return toolResult{}, err
			}
			poll := discord.NewPollCreate(in.Question)
			for _, a := range in.Answers {
				poll = poll.AddAnswer(a, nil)
			}
			poll = poll.WithDuration(in.DurationHours).WithAllowMultiselect(in.AllowMultiselect)
			if _, err := b.client.Rest.CreateMessage(chID, discord.NewMessageCreate().WithPoll(poll)); err != nil {
				return toolResult{}, err
			}
			return toolResult{content: "poll created"}, nil
		},
	}
}

func (b *Bot) getVoiceStateTool() ricktool {
	return ricktool{
		name: "get_voice_state",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "get_voice_state",
				Description: anthropic.String("See who is currently in a voice channel and which channel they are in."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{},
				},
			},
		},
		execute: func(_ context.Context, _ json.RawMessage) (toolResult, error) {
			var sb strings.Builder
			b.voiceChannels.Range(func(key, val any) bool {
				uid := key.(snowflake.ID)
				chName := val.(string)
				if chName == "" {
					chName = "unknown channel"
				}
				name, err := b.GetUsernameForID(uid.String())
				if err != nil {
					name = uid.String()
				}
				fmt.Fprintf(&sb, "%s is in #%s\n", name, chName)
				return true
			})
			if sb.Len() == 0 {
				return toolResult{content: "nobody is in voice"}, nil
			}
			return toolResult{content: sb.String()}, nil
		},
	}
}

func (b *Bot) sendFileTool() ricktool {
	return ricktool{
		name: "send_file",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name: "send_file",
				Description: anthropic.String("Attach and send a file from the work directory (/rick/work) to any channel. " +
					"Use after shell_exec writes output to a file when the content is too long for a message."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"channel_id": map[string]any{
							"type":        "string",
							"description": "Discord channel snowflake ID to post the file in.",
						},
						"path": map[string]any{
							"type":        "string",
							"description": "Absolute path to the file inside /rick/work, e.g. /rick/work/output.txt.",
						},
						"caption": map[string]any{
							"type":        "string",
							"description": "Optional message to accompany the file.",
						},
					},
					Required: []string{"channel_id", "path"},
				},
			},
		},
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				ChannelID string `json:"channel_id"`
				Path      string `json:"path"`
				Caption   string `json:"caption"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}

			// restrict to work dir
			clean := filepath.Clean(in.Path)
			if !strings.HasPrefix(clean, b.config.WorkDir) {
				return toolResult{content: "path must be inside " + b.config.WorkDir}, nil
			}

			f, err := os.Open(clean)
			if err != nil {
				return toolResult{}, err
			}
			defer func() {
				if cerr := f.Close(); cerr != nil {
					slog.Warn("failed to close file after send", "error", cerr)
				}
			}()

			msg := discord.NewMessageCreate().AddFile(filepath.Base(clean), "", f)
			if in.Caption != "" {
				msg = msg.WithContent(in.Caption)
			}

			chID, err := snowflake.Parse(in.ChannelID)
			if err != nil {
				return toolResult{}, err
			}
			if _, err := b.client.Rest.CreateMessage(chID, msg); err != nil {
				return toolResult{}, err
			}
			return toolResult{content: "file sent"}, nil
		},
	}
}

func (b *Bot) sendEmbedTool() ricktool {
	return ricktool{
		name: "send_embed",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "send_embed",
				Description: anthropic.String("Send a rich embed card to any channel. Good for formatted data, stats, or anything that deserves a nice card."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"channel_id": map[string]any{
							"type":        "string",
							"description": "Discord channel snowflake ID.",
						},
						"title": map[string]any{
							"type":        "string",
							"description": "Embed title.",
						},
						"description": map[string]any{
							"type":        "string",
							"description": "Embed body text. Supports Discord markdown.",
						},
						"color": map[string]any{
							"type":        "integer",
							"description": "Embed sidebar color as a decimal integer (e.g. 0xFF0000 = 16711680 for red).",
						},
						"fields": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"name":   map[string]any{"type": "string"},
									"value":  map[string]any{"type": "string"},
									"inline": map[string]any{"type": "boolean"},
								},
								"required": []string{"name", "value"},
							},
							"description": "Optional list of field rows.",
						},
						"caption": map[string]any{
							"type":        "string",
							"description": "Optional plain-text message above the embed.",
						},
					},
					Required: []string{"channel_id"},
				},
			},
		},
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				ChannelID   string `json:"channel_id"`
				Title       string `json:"title"`
				Description string `json:"description"`
				Color       int    `json:"color"`
				Fields      []struct {
					Name   string `json:"name"`
					Value  string `json:"value"`
					Inline bool   `json:"inline"`
				} `json:"fields"`
				Caption string `json:"caption"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			chID, err := snowflake.Parse(in.ChannelID)
			if err != nil {
				return toolResult{}, err
			}
			embed := discord.Embed{
				Title:       in.Title,
				Description: in.Description,
				Color:       in.Color,
			}
			for _, f := range in.Fields {
				embed.Fields = append(embed.Fields, discord.EmbedField{
					Name:   f.Name,
					Value:  f.Value,
					Inline: &f.Inline,
				})
			}
			msg := discord.NewMessageCreate().WithEmbeds(embed)
			if in.Caption != "" {
				msg = msg.WithContent(in.Caption)
			}
			if _, err := b.client.Rest.CreateMessage(chID, msg); err != nil {
				return toolResult{}, err
			}
			return toolResult{content: "embed sent"}, nil
		},
	}
}

func (b *Bot) searchMembersTool() ricktool {
	return ricktool{
		name: "search_members",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "search_members",
				Description: anthropic.String("Search for guild members by display name or username. Returns their IDs and display names."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "Name substring to search for (case-insensitive).",
						},
					},
					Required: []string{"query"},
				},
			},
		},
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				Query string `json:"query"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			q := strings.ToLower(in.Query)
			// ensure cache is populated via the existing lazy-load path
			_, _ = b.GetMemberForID("")
			b.cache.mu.RLock()
			members := b.cache.members
			b.cache.mu.RUnlock()

			var sb strings.Builder
			for _, m := range members {
				name := m.EffectiveName()
				if strings.Contains(strings.ToLower(name), q) || strings.Contains(strings.ToLower(m.User.Username), q) {
					fmt.Fprintf(&sb, "%s %s\n", m.User.ID, name)
				}
			}
			if sb.Len() == 0 {
				return toolResult{content: "no members found"}, nil
			}
			return toolResult{content: sb.String()}, nil
		},
	}
}

func (b *Bot) getRolesTool() ricktool {
	return ricktool{
		name: "get_roles",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "get_roles",
				Description: anthropic.String("List all roles available on the server with their IDs, names, and colors."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{},
				},
			},
		},
		execute: func(_ context.Context, _ json.RawMessage) (toolResult, error) {
			guildID, err := snowflake.Parse(b.config.DiscordGuild)
			if err != nil {
				return toolResult{}, err
			}
			roles, err := b.client.Rest.GetRoles(guildID)
			if err != nil {
				return toolResult{}, err
			}
			var sb strings.Builder
			for _, r := range roles {
				fmt.Fprintf(&sb, "%s %s (color: #%06X)\n", r.ID, r.Name, r.Color)
			}
			if sb.Len() == 0 {
				return toolResult{content: "no roles found"}, nil
			}
			return toolResult{content: sb.String()}, nil
		},
	}
}

func (b *Bot) getMemberRolesTool() ricktool {
	return ricktool{
		name: "get_member_roles",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "get_member_roles",
				Description: anthropic.String("Get the roles assigned to a specific guild member by their Discord snowflake ID."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"user_id": map[string]any{
							"type":        "string",
							"description": "Discord snowflake of the member to look up.",
						},
					},
					Required: []string{"user_id"},
				},
			},
		},
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				UserID string `json:"user_id"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			member, err := b.GetMemberForID(in.UserID)
			if err != nil {
				return toolResult{}, err
			}
			guildID, err := snowflake.Parse(b.config.DiscordGuild)
			if err != nil {
				return toolResult{}, err
			}
			roles, err := b.client.Rest.GetRoles(guildID)
			if err != nil {
				return toolResult{}, err
			}
			roleByID := make(map[snowflake.ID]string, len(roles))
			for _, r := range roles {
				roleByID[r.ID] = r.Name
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "%s roles:\n", member.EffectiveName())
			for _, rid := range member.RoleIDs {
				name, ok := roleByID[rid]
				if !ok {
					name = rid.String()
				}
				fmt.Fprintf(&sb, "  %s %s\n", rid, name)
			}
			if len(member.RoleIDs) == 0 {
				sb.WriteString("  (no roles)\n")
			}
			return toolResult{content: sb.String()}, nil
		},
	}
}

func (b *Bot) scheduleReminderTool() ricktool {
	return ricktool{
		name: "schedule_reminder",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name: "schedule_reminder",
				Description: anthropic.String("Schedule a one-shot reminder that will be posted in a channel at a specific time. " +
					"Use the current time from the system prompt to compute fire_at. " +
					"The reminder will mention the target user."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"channel_id": map[string]any{
							"type":        "string",
							"description": "Channel to post the reminder in.",
						},
						"user_id": map[string]any{
							"type":        "string",
							"description": "Discord snowflake of the user to remind.",
						},
						"message": map[string]any{
							"type":        "string",
							"description": "Reminder message text.",
						},
						"fire_at": map[string]any{
							"type":        "string",
							"description": "ISO 8601 UTC timestamp when to fire the reminder, e.g. 2006-01-02T15:04:05Z.",
						},
					},
					Required: []string{"channel_id", "user_id", "message", "fire_at"},
				},
			},
		},
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				ChannelID string `json:"channel_id"`
				UserID    string `json:"user_id"`
				Message   string `json:"message"`
				FireAt    string `json:"fire_at"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			fireAt, err := time.Parse(time.RFC3339, in.FireAt)
			if err != nil {
				return toolResult{content: "invalid fire_at format, use ISO 8601 e.g. 2006-01-02T15:04:05Z"}, nil
			}
			if fireAt.Before(time.Now()) {
				return toolResult{content: "fire_at is in the past"}, nil
			}
			r := store.Reminder{
				ChannelID: in.ChannelID,
				UserID:    in.UserID,
				Message:   in.Message,
				FireAt:    fireAt,
			}
			if err := b.store.CreateReminder(r); err != nil {
				return toolResult{}, err
			}
			return toolResult{content: fmt.Sprintf("reminder scheduled for %s", fireAt.Format(time.RFC3339))}, nil
		},
	}
}
