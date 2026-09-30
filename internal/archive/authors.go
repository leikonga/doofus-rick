package archive

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/leikonga/doofus-rick/internal/store"
)

type AuthorFinder interface {
	FindAuthorsByName(ctx context.Context, name string) ([]store.ActiveAuthor, error)
}

// ResolveAuthor returns a nil ID and a model-readable message when the author cannot be resolved to exactly one user.
func ResolveAuthor(ctx context.Context, finder AuthorFinder, author string) (*uint64, string, error) {
	author = strings.TrimSpace(author)
	if author == "" {
		return nil, "", nil
	}
	if id, err := strconv.ParseUint(author, 10, 64); err == nil {
		return &id, "", nil
	}
	matches, err := finder.FindAuthorsByName(ctx, author)
	if err != nil {
		return nil, "", err
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Sprintf("no author named %q found; pass a Discord snowflake or an exact display name", author), nil
	case 1:
		return &matches[0].AuthorID, "", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "author %q is ambiguous; retry with one of these snowflakes:\n", author)
	for _, m := range matches {
		fmt.Fprintf(&sb, "- %d (%s, %d messages)\n", m.AuthorID, m.AuthorName, m.MsgCount)
	}
	return nil, sb.String(), nil
}
