package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

func (a *Agent) buildTools(event *events.MessageCreate) llm.Tools {
	tools := llm.Tools{
		a.declineTool(),
		a.checkLogsTool(),
		a.mediaSearchTool(),
		a.webSearchTool(),
		a.fetchPageTool(),
		a.shellExecTool(),
		a.reactTool(event),
		a.saveQuoteTool(event),
		a.getUserQuotesTool(),
		a.searchQuotesTool(),
	}
	return append(tools, a.discordTools()...)
}

type declineIn struct {
	Emoji string `json:"emoji" jsonschema:"description=Unicode emoji to react with."`
}

func (a *Agent) declineTool() llm.Tool {
	return llm.NewTool("decline", "Decline to respond and optionally react with an emoji instead.",
		func(_ context.Context, in declineIn) (llm.Result, error) {
			return llm.Result{Response: &llm.RickResponse{Decline: true, Emoji: in.Emoji}}, nil
		})
}

type mediaSearchIn struct {
	Type    string `json:"type" jsonschema:"required,enum=gif,enum=image,description=Kind of media to search for."`
	Query   string `json:"query" jsonschema:"required,description=Search query. Supports site: operators for images."`
	Caption string `json:"caption" jsonschema:"description=Optional short text to post alongside the result."`
}

func (a *Agent) mediaSearchTool() llm.Tool {
	return llm.NewTool("media_search", "Search for a GIF or a static image and post it as a response.",
		func(ctx context.Context, in mediaSearchIn) (llm.Result, error) {
			var url string
			var err error
			switch in.Type {
			case "gif":
				url, err = a.giphy.Search(ctx, in.Query)
			case "image":
				url, err = a.brave.SearchImage(ctx, in.Query)
			default:
				return llm.Result{}, fmt.Errorf("unknown media type %q, must be gif or image", in.Type)
			}
			if err != nil {
				return llm.Result{}, err
			}
			text := url
			if in.Caption != "" {
				text = in.Caption + "\n" + url
			}
			return llm.Result{Response: &llm.RickResponse{Text: text}}, nil
		})
}

type webSearchIn struct {
	Query     string `json:"query" jsonschema:"required,description=Search query."`
	Freshness string `json:"freshness" jsonschema:"description=Restrict results by age: pd=24h, pw=7 days, pm=31 days, py=1 year."`
}

func (a *Agent) webSearchTool() llm.Tool {
	return llm.NewTool("web_search",
		"Search the web and get titles, URLs, and descriptions from the top results. "+
			"Supports standard operators like site:, intitle:, etc. "+
			"Use fetch_page to read the full content of any result URL.",
		func(ctx context.Context, in webSearchIn) (llm.Result, error) {
			result, err := a.brave.Search(ctx, in.Query, in.Freshness)
			if err != nil {
				return llm.Result{}, err
			}
			return llm.Result{Content: result}, nil
		})
}

type fetchPageIn struct {
	URL string `json:"url" jsonschema:"required,description=URL to fetch."`
}

func (a *Agent) fetchPageTool() llm.Tool {
	return llm.NewTool("fetch_page", "Fetch and read the text content of a web page. Use after web_search to dig into a specific result.",
		func(ctx context.Context, in fetchPageIn) (llm.Result, error) {
			content, err := a.brave.FetchPage(ctx, in.URL)
			if err != nil {
				return llm.Result{}, err
			}
			return llm.Result{Content: content}, nil
		})
}

type shellExecIn struct {
	Command string `json:"command" jsonschema:"required,description=Shell command to run."`
}

func (a *Agent) shellExecTool() llm.Tool {
	return llm.NewTool("shell_exec",
		"Run a shell command and return stdout+stderr. "+
			"Runs as an unprivileged user in an Alpine Linux environment. "+
			"Available: bash, curl, jq, git, openssh-client, python3, uv, make, coreutils, sqlite3, diffutils, patch, bc, file, dig, openssl, imagemagick. "+
			"Working directory is /rick/work; persistent across calls, use it freely to store files, scripts, databases, cloned repos, etc. "+
			"HOME is also /rick/work. "+
			"Python packages can be installed inline with: uv run --with <pkg> python3 -c '...'.",
		func(ctx context.Context, in shellExecIn) (llm.Result, error) {
			return llm.Result{Content: a.shell.Exec(ctx, in.Command)}, nil
		})
}

type saveQuoteIn struct {
	Content        string   `json:"content" jsonschema:"required,description=The quote text to save."`
	ParticipantIDs []string `json:"participant_ids" jsonschema:"description=Discord snowflakes of any additional participants in the quote."`
}

func (a *Agent) saveQuoteTool(event *events.MessageCreate) llm.Tool {
	return llm.NewTool("save_quote", "Save a quote to the quote book and display it as a quote embed. Use when someone says something memorable or worth archiving.",
		func(ctx context.Context, in saveQuoteIn) (llm.Result, error) {
			creatorID := event.Message.Author.ID.String()
			q := store.Quote{
				Content:      in.Content,
				Creator:      creatorID,
				Participants: (*store.StringSlice)(&in.ParticipantIDs),
			}
			if err := a.store.CreateQuote(ctx, q); err != nil {
				return llm.Result{}, err
			}

			author, err := a.discord.GetMemberForID(creatorID)
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

			return llm.Result{Content: "quote saved", Done: true}, nil
		})
}

type getUserQuotesIn struct {
	UserID string `json:"user_id" jsonschema:"required,description=Discord snowflake of the user to look up."`
}

func (a *Agent) getUserQuotesTool() llm.Tool {
	return llm.NewTool("get_user_quotes", "Look up all saved quotes for a user by their Discord snowflake. Use to find ammunition for roasting someone.",
		func(ctx context.Context, in getUserQuotesIn) (llm.Result, error) {
			quotes := a.store.GetQuotesByParticipant(ctx, in.UserID)
			if len(quotes) == 0 {
				return llm.Result{Content: "no quotes found for this user"}, nil
			}
			var sb strings.Builder
			for _, q := range quotes {
				fmt.Fprintf(&sb, "- [%s] %s\n", q.CreatedAt.Format("2006-01-02"), q.Content)
			}
			return llm.Result{Content: sb.String()}, nil
		})
}

type searchQuotesIn struct {
	Query string `json:"query" jsonschema:"required,description=Text to search for within quote content."`
}

func (a *Agent) searchQuotesTool() llm.Tool {
	return llm.NewTool("search_quotes", "Search the quote book by content. Use when looking for a specific quote or when a user has no participant quotes on record.",
		func(ctx context.Context, in searchQuotesIn) (llm.Result, error) {
			quotes := a.store.SearchQuotes(ctx, in.Query)
			if len(quotes) == 0 {
				return llm.Result{Content: "no quotes found"}, nil
			}
			var sb strings.Builder
			for _, q := range quotes {
				fmt.Fprintf(&sb, "- [%s] %s\n", q.CreatedAt.Format("2006-01-02"), q.Content)
			}
			return llm.Result{Content: sb.String()}, nil
		})
}

type reactIn struct {
	Emojis []string `json:"emojis" jsonschema:"required,description=Unicode emojis to react with."`
}

func (a *Agent) reactTool(event *events.MessageCreate) llm.Tool {
	return llm.NewTool("react", "Add one or more emoji reactions to the message you are replying to. Can be used alongside a text response.",
		func(_ context.Context, in reactIn) (llm.Result, error) {
			for _, emoji := range in.Emojis {
				if err := event.Client().Rest.AddReaction(event.ChannelID, event.MessageID, emoji); err != nil {
					slog.Warn("failed to add reaction", "emoji", emoji, "error", err)
				}
			}
			return llm.Result{Content: "reactions added"}, nil
		})
}

type checkLogsIn struct{}

func (a *Agent) checkLogsTool() llm.Tool {
	return llm.NewTool("check_logs", "Check recent warnings and errors from Rick's own process logs. Use when asked why Rick didn't respond or what went wrong.",
		func(_ context.Context, _ checkLogsIn) (llm.Result, error) {
			return llm.Result{Content: a.logBuf.Recent()}, nil
		})
}

func memberEmbedFooter(member *discord.Member, fallbackID string) *discord.EmbedFooter {
	if member == nil {
		return &discord.EmbedFooter{Text: fallbackID}
	}
	return &discord.EmbedFooter{
		Text:    member.EffectiveName(),
		IconURL: member.User.EffectiveAvatarURL(),
	}
}
