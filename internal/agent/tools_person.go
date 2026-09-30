package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

type personIn struct {
	User string `json:"user" jsonschema:"required,description=The member to look up. A Discord snowflake or an exact display name."`
}

func (a *Agent) personTool() llm.Tool {
	return llm.NewTool("memory_person", "Read the maintained profile of a server member: how they talk, recurring topics, running jokes, nicknames, who they interact with and notable quotes, built from public channels only. Use it before writing about, roasting, or making content about a specific member.",
		func(ctx context.Context, in personIn) (llm.Result, error) {
			if strings.TrimSpace(in.User) == "" {
				return llm.Result{}, errors.New("user is required")
			}
			userID, notice, err := a.resolveAuthor(ctx, in.User)
			if err != nil {
				return llm.Result{}, err
			}
			if notice != "" {
				return llm.Continue(notice), nil
			}
			p, err := a.store.GetPersonProfile(ctx, *userID)
			if errors.Is(err, store.ErrNotFound) {
				return llm.Continue(fmt.Sprintf("no profile for %d yet; fall back to memory_search with the author filter", *userID)), nil
			}
			if err != nil {
				return llm.Result{}, err
			}
			return llm.Continue(fmt.Sprintf("%s\n\nlast updated %s", p.Summary, p.UpdatedAt.Format(dateLayout))), nil
		})
}
