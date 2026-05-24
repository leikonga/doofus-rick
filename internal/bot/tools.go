package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/leikonga/doofus-rick/internal/store"
)

type rickResponse struct {
	text    string
	decline bool
	emoji   string
	embed   *discord.Embed
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
	tools := []ricktool{
		b.declineTool(),
		b.mediaResponseTool("gif_search", "Search for a GIF and post it as a response.", b.searchGiphy),
		b.webSearchTool(),
		b.fetchPageTool(),
		b.rememberTool(),
		b.recallTool(),
		b.shellExecTool(),
		b.mediaResponseTool("image_search", "Search for a static image and post it as a response. Supports site: operators.", b.searchBraveImage),
		b.reactTool(event),
		b.saveQuoteTool(event),
		b.getUserQuotesTool(),
		b.searchQuotesTool(),
	}
	return append(tools, b.discordTools()...)
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
				Name: "web_search",
				Description: anthropic.String("Search the web and get titles, URLs, and descriptions from the top results. " +
					"Supports standard operators like site:, intitle:, etc. " +
					"Use fetch_page to read the full content of any result URL. " +
					"Set freshness to 'pd' (24h), 'pw' (7d), 'pm' (31d), or 'py' (1y) to restrict to recent content."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "Search query.",
						},
						"freshness": map[string]any{
							"type":        "string",
							"description": "Restrict results by age: pd=24h, pw=7 days, pm=31 days, py=1 year.",
						},
					},
					Required: []string{"query"},
				},
			},
		},
		execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				Query     string `json:"query"`
				Freshness string `json:"freshness"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			result, err := b.searchBrave(ctx, in.Query, in.Freshness)
			if err != nil {
				return toolResult{}, err
			}
			return toolResult{content: result}, nil
		},
	}
}

func (b *Bot) fetchPageTool() ricktool {
	return ricktool{
		name: "fetch_page",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "fetch_page",
				Description: anthropic.String("Fetch and read the text content of a web page. Use after web_search to dig into a specific result."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"url": map[string]any{
							"type":        "string",
							"description": "URL to fetch.",
						},
					},
					Required: []string{"url"},
				},
			},
		},
		execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			content, err := b.fetchPage(ctx, in.URL)
			if err != nil {
				return toolResult{}, err
			}
			return toolResult{content: content}, nil
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
		execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				Content string   `json:"content"`
				UserID  string   `json:"user_id"`
				Tags    []string `json:"tags"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			if err := b.store.SaveMemory(ctx, in.UserID, in.Content, in.Tags); err != nil {
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
		execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				Query  string `json:"query"`
				UserID string `json:"user_id"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}
			memories, err := b.store.SearchMemory(ctx, in.Query, in.UserID)
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
				Description: anthropic.String("Run a shell command and return stdout+stderr. " +
					"Runs as an unprivileged user in an Alpine Linux environment. " +
					"Available: bash, curl, jq, git, openssh-client, python3, uv, make, coreutils, sqlite3, diffutils, patch, bc, file, dig, openssl, imagemagick. " +
					"Working directory is /rick/work — persistent across calls; use it freely to store files, scripts, databases, cloned repos, etc. " +
					"HOME is also /rick/work. " +
					"Python packages can be installed inline with: uv run --with <pkg> python3 -c '...'."),
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

func (b *Bot) saveQuoteTool(event *events.MessageCreate) ricktool {
	return ricktool{
		name: "save_quote",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "save_quote",
				Description: anthropic.String("Save a quote to the quote book and display it as a quote embed. Use when someone says something memorable or worth archiving."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"content": map[string]any{
							"type":        "string",
							"description": "The quote text to save.",
						},
						"participant_ids": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Discord snowflakes of any additional participants in the quote.",
						},
					},
					Required: []string{"content"},
				},
			},
		},
		execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				Content        string   `json:"content"`
				ParticipantIDs []string `json:"participant_ids"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}

			creatorID := event.Message.Author.ID.String()
			q := store.Quote{
				Content:      in.Content,
				Creator:      creatorID,
				Participants: in.ParticipantIDs,
			}
			if err := b.store.CreateQuote(ctx, q); err != nil {
				return toolResult{}, err
			}

			author, err := b.GetMemberForID(creatorID)
			if err != nil {
				slog.Warn("failed to get author for saved quote", "error", err)
			}
			now := time.Now()
			embed := discord.Embed{
				Description: in.Content,
				Color:       0x11806A,
				Timestamp:   &now,
				Footer:      memberEmbedFooter(author, creatorID),
			}

			_, sendErr := event.Client().Rest.CreateMessage(event.ChannelID, discord.NewMessageCreate().
				WithEmbeds(embed).
				WithMessageReferenceByID(event.MessageID),
			)
			if sendErr != nil {
				slog.Warn("failed to send quote embed", "error", sendErr)
			}

			return toolResult{content: "quote saved"}, nil
		},
	}
}

func (b *Bot) getUserQuotesTool() ricktool {
	return ricktool{
		name: "get_user_quotes",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "get_user_quotes",
				Description: anthropic.String("Look up all saved quotes for a user by their Discord snowflake. Use to find ammunition for roasting someone."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"user_id": map[string]any{
							"type":        "string",
							"description": "Discord snowflake of the user to look up.",
						},
					},
					Required: []string{"user_id"},
				},
			},
		},
		execute: func(ctx context.Context, input json.RawMessage) (toolResult, error) {
			var in struct {
				UserID string `json:"user_id"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return toolResult{}, err
			}

			quotes := b.store.GetQuotesByParticipant(ctx, in.UserID)
			if len(quotes) == 0 {
				return toolResult{content: "no quotes found for this user"}, nil
			}

			var sb strings.Builder
			for _, q := range quotes {
				fmt.Fprintf(&sb, "- [%s] %s\n", q.CreatedAt.Format("2006-01-02"), q.Content)
			}
			return toolResult{content: sb.String()}, nil
		},
	}
}

func (b *Bot) searchQuotesTool() ricktool {
	return ricktool{
		name: "search_quotes",
		def: anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        "search_quotes",
				Description: anthropic.String("Search the quote book by content. Use when looking for a specific quote or when a user has no participant quotes on record."),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "Text to search for within quote content.",
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
			quotes := b.store.SearchQuotes(ctx, in.Query)
			if len(quotes) == 0 {
				return toolResult{content: "no quotes found"}, nil
			}
			var sb strings.Builder
			for _, q := range quotes {
				fmt.Fprintf(&sb, "- [%s] %s\n", q.CreatedAt.Format("2006-01-02"), q.Content)
			}
			return toolResult{content: sb.String()}, nil
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
