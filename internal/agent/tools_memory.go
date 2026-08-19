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

type saveQuoteIn struct {
	Content        string   `json:"content" jsonschema:"required,description=The quote text to save."`
	ParticipantIDs []string `json:"participant_ids" jsonschema:"description=Discord snowflakes of any additional participants in the quote."`
}

func (a *Agent) saveQuoteTool(event *events.MessageCreate) llm.Tool {
	return llm.NewTool("memory_quote_save", "Save a quote to the quote book and display it as a quote embed. Use when someone says something memorable or worth archiving.",
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

type searchHistoryIn struct {
	Query string `json:"query" jsonschema:"required,description=What to search for."`
	Scope string `json:"scope" jsonschema:"required,enum=messages,enum=quotes,description=messages searches archived chat history via hybrid retrieval; quotes searches the quote book."`
}

// searchHistoryTool is the deliberate counterpart to the automatic recall
// pre-fetch injected into every prompt: nothing depends on Rick calling
// this, it's for digging on purpose when the automatic context missed
// something. Folds the old search_quotes tool in via the scope param.
func (a *Agent) searchHistoryTool(event *events.MessageCreate) llm.Tool {
	return llm.NewTool("memory_search", "Search either the archived chat history or the quote book for something specific. Use for deliberate digging when the automatic context didn't surface what you need.",
		func(ctx context.Context, in searchHistoryIn) (llm.Result, error) {
			switch in.Scope {
			case "quotes":
				quotes := a.store.SearchQuotes(ctx, in.Query)
				if len(quotes) == 0 {
					return llm.Result{Content: "no matching quotes found"}, nil
				}
				var sb strings.Builder
				for _, q := range quotes {
					fmt.Fprintf(&sb, "- [%s] %s\n", q.CreatedAt.Format("2006-01-02"), q.Content)
				}
				return llm.Result{Content: sb.String()}, nil
			case "messages", "":
				channelIDs := a.visibleChannelIDs(event.Message.Author.ID)
				if len(channelIDs) == 0 {
					return llm.Result{Content: "no channels to search"}, nil
				}
				chunks, err := a.retriever.Retrieve(ctx, in.Query, channelIDs)
				if err != nil {
					return llm.Result{}, err
				}
				if len(chunks) == 0 {
					return llm.Result{Content: "no matching history found"}, nil
				}
				var sb strings.Builder
				for _, c := range chunks {
					sb.WriteString(c.Content)
					sb.WriteString("\n---\n")
				}
				return llm.Result{Content: sb.String()}, nil
			default:
				return llm.Result{}, fmt.Errorf("unknown scope %q, must be messages or quotes", in.Scope)
			}
		})
}
