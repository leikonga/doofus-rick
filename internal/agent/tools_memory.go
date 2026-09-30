package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/leikonga/doofus-rick/internal/archive"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

type saveQuoteIn struct {
	Content        string   `json:"content" jsonschema:"required,description=The quote text to save."`
	ParticipantIDs []string `json:"participant_ids" jsonschema:"description=Discord snowflakes of any additional participants in the quote."`
}

func (a *Agent) saveQuoteTool(origin turnOrigin) llm.Tool {
	return llm.NewTool("memory_quote_save", "Save a quote to the quote book and display it as a quote embed. Use when someone says something memorable or worth archiving.",
		func(ctx context.Context, in saveQuoteIn) (llm.Result, error) {
			creatorID := origin.AuthorID.String()
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

			msg := discord.NewMessageCreate().WithEmbeds(embed)
			if origin.MessageID != 0 {
				msg = msg.WithMessageReferenceByID(origin.MessageID)
			}
			if _, sendErr := a.discordClient.Rest.CreateMessage(origin.ChannelID, msg, rest.WithCtx(ctx)); sendErr != nil {
				slog.Warn("failed to send quote embed", "error", sendErr)
			}

			return llm.EndTurn("quote saved"), nil
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

type getUserQuotesIn struct {
	UserID string `json:"user_id" jsonschema:"required,description=Discord snowflake of the user to look up."`
}

func (a *Agent) getUserQuotesTool() llm.Tool {
	return llm.NewTool("memory_quote_list", "Look up all saved quotes for a user by their Discord snowflake. Use to find ammunition for roasting someone.",
		func(ctx context.Context, in getUserQuotesIn) (llm.Result, error) {
			quotes, err := a.store.GetQuotesByParticipant(ctx, in.UserID)
			if err != nil {
				slog.Warn("failed to get quotes by participant", "user_id", in.UserID, "error", err)
			}
			if len(quotes) == 0 {
				return llm.Continue("no quotes found for this user"), nil
			}
			var sb strings.Builder
			for _, q := range quotes {
				fmt.Fprintf(&sb, "- [%s] %s\n", q.CreatedAt.Format("2006-01-02"), q.Content)
			}
			return llm.Continue(sb.String()), nil
		})
}

type searchHistoryIn struct {
	Query  string `json:"query" jsonschema:"required,description=What to search for."`
	Scope  string `json:"scope" jsonschema:"required,enum=messages,enum=quotes,description=messages searches archived chat history via hybrid retrieval; quotes searches the quote book."`
	Author string `json:"author,omitempty" jsonschema:"description=Only for scope messages: restrict to chunks containing messages by this author. A Discord snowflake or an exact display name."`
	Since  string `json:"since,omitempty" jsonschema:"description=Only for scope messages: earliest date inclusive as YYYY-MM-DD."`
	Until  string `json:"until,omitempty" jsonschema:"description=Only for scope messages: latest date inclusive as YYYY-MM-DD."`
}

const dateLayout = "2006-01-02"

func parseSearchDates(since, until string) (*time.Time, *time.Time, error) {
	var from, to *time.Time
	if since != "" {
		t, err := time.Parse(dateLayout, since)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid since %q, expected YYYY-MM-DD", since)
		}
		from = &t
	}
	if until != "" {
		t, err := time.Parse(dateLayout, until)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid until %q, expected YYYY-MM-DD", until)
		}
		t = t.AddDate(0, 0, 1)
		to = &t
	}
	return from, to, nil
}

func (a *Agent) resolveAuthor(ctx context.Context, author string) (*uint64, string, error) {
	return archive.ResolveAuthor(ctx, a.store, author)
}

func (a *Agent) searchHistoryTool(origin turnOrigin) llm.Tool {
	return llm.NewTool("memory_search", "Search either the archived chat history or the quote book for something specific. Use for deliberate digging when the automatic context didn't surface what you need. For scope messages, optional author (snowflake or exact display name) and since/until (YYYY-MM-DD, inclusive) narrow the search.",
		func(ctx context.Context, in searchHistoryIn) (llm.Result, error) {
			switch in.Scope {
			case "quotes":
				quotes, err := a.store.SearchQuotes(ctx, in.Query)
				if err != nil {
					slog.Warn("failed to search quotes", "query", in.Query, "error", err)
				}
				if len(quotes) == 0 {
					return llm.Continue("no matching quotes found"), nil
				}
				var sb strings.Builder
				for _, q := range quotes {
					fmt.Fprintf(&sb, "- [%s] %s\n", q.CreatedAt.Format("2006-01-02"), q.Content)
				}
				return llm.Continue(sb.String()), nil
			case "messages", "":
				channelIDs := a.visibleChannelIDs(ctx, origin.AuthorID)
				if len(channelIDs) == 0 {
					return llm.Continue("no channels to search"), nil
				}
				since, until, err := parseSearchDates(in.Since, in.Until)
				if err != nil {
					return llm.Result{}, err
				}
				authorID, notice, err := a.resolveAuthor(ctx, in.Author)
				if err != nil {
					return llm.Result{}, err
				}
				if notice != "" {
					return llm.Continue(notice), nil
				}
				chunks, err := a.retriever.Retrieve(ctx, archive.RetrieveRequest{
					Query:      in.Query,
					ChannelIDs: channelIDs,
					AuthorID:   authorID,
					Since:      since,
					Until:      until,
				})
				if err != nil {
					return llm.Result{}, err
				}
				if len(chunks) == 0 {
					return llm.Continue("no matching history found"), nil
				}
				var sb strings.Builder
				for _, c := range chunks {
					sb.WriteString(c.Content)
					sb.WriteString("\n---\n")
				}
				return llm.Continue(sb.String()), nil
			default:
				return llm.Result{}, fmt.Errorf("unknown scope %q, must be messages or quotes", in.Scope)
			}
		})
}
