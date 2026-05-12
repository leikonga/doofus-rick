package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/disgoorg/disgo/events"
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

func (b *Bot) buildTools(event *events.MessageCreate) []ricktool {
	return []ricktool{
		b.declineTool(),
		b.mediaResponseTool("gif_search", "Search for a GIF and post it as a response.", b.searchGiphy),
		b.webSearchTool(),
		b.rememberTool(),
		b.recallTool(),
		b.shellExecTool(),
		b.mediaResponseTool("image_search", "Search for a static image and post it as a response. Supports site: operators.", b.searchBraveImage),
		b.reactTool(event),
	}
}

func (b *Bot) declineTool() ricktool {
	return ricktool{
		name: "decline",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "decline",
				Description: anthropic.String("Decline to respond and optionally react with an emoji instead."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"emoji": map[string]any{
							"type":        "string",
							"description": "Unicode emoji to react with.",
						},
					},
				},
			},
		},
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				Emoji string `json:"emoji"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			return toolResult{response: &rickResponse{decline: true, emoji: in.Emoji}}, nil
		},
	}
}

// mediaResponseTool builds a gif_search or image_search tool backed by the given fetch function.
func (b *Bot) mediaResponseTool(name, desc string, fetch func(context.Context, string) (string, error)) ricktool {
	return ricktool{
		name: name,
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        name,
				Description: anthropic.String(desc),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "Search query.",
						},
						"caption": map[string]any{
							"type":        "string",
							"description": "Optional short text to post alongside the result.",
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
			url, err := fetch(ctx, in.Query)
			if err != nil {
				return toolResult{}, err
			}
			text := url
			if in.Caption != "" {
				text = in.Caption + "\n" + url
			}
			return toolResult{response: &rickResponse{text: text}}, nil
		},
	}
}

func (b *Bot) webSearchTool() ricktool {
	return ricktool{
		name: "web_search",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "web_search",
				Description: anthropic.String("Search the web and get extracted content from the top results. Supports standard operators like site:, intitle:, etc."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "Search query.",
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
	}
}

func (b *Bot) rememberTool() ricktool {
	return ricktool{
		name: "remember",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "remember",
				Description: anthropic.String("Save a piece of text to persistent memory, optionally associated with a user."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"content": map[string]any{
							"type":        "string",
							"description": "Text to store.",
						},
						"user_id": map[string]any{
							"type":        "string",
							"description": "Discord user ID to associate this memory with.",
						},
						"tags": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Tags for filtering during recall.",
						},
					},
					Required: []string{"content"},
				},
			},
		},
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
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
	}
}

func (b *Bot) recallTool() ricktool {
	return ricktool{
		name: "recall",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "recall",
				Description: anthropic.String("Search persistent memory by keyword, optionally filtered by user."),
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
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
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
				fmt.Fprintf(&sb, "- [%s] %s\n", m.CreatedAt.Format("2006-01-02"), m.Content)
			}
			return toolResult{content: sb.String()}, nil
		},
	}
}

func (b *Bot) shellExecTool() ricktool {
	return ricktool{
		name: "shell_exec",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name: "shell_exec",
				Description: anthropic.String("Run a shell command and return stdout. " +
					"Runs inside an Alpine Linux container as an unprivileged user; " +
					"busybox utilities are available (sh, ls, ps, df, free, uptime, uname, cat, date, wget, etc.)."),
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
	}
}

func (b *Bot) reactTool(event *events.MessageCreate) ricktool {
	return ricktool{
		name: "react",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "react",
				Description: anthropic.String("Add one or more emoji reactions to the message you are replying to. Can be used alongside a text response."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"emojis": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Unicode emojis to react with.",
						},
					},
					Required: []string{"emojis"},
				},
			},
		},
		execute: func(_ context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				Emojis []string `json:"emojis"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			for _, emoji := range in.Emojis {
				if err := event.Client().Rest.AddReaction(event.ChannelID, event.MessageID, emoji); err != nil {
					slog.Warn("failed to add reaction", "emoji", emoji, "error", err)
				}
			}
			return toolResult{content: "reactions added"}, nil
		},
	}
}
