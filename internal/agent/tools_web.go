package agent

import (
	"context"
	"fmt"

	"github.com/leikonga/doofus-rick/internal/brave"
	"github.com/leikonga/doofus-rick/internal/giphy"
	"github.com/leikonga/doofus-rick/internal/llm"
)

type webTools struct {
	brave *brave.Client
	giphy *giphy.Client
}

type mediaSearchIn struct {
	Type    string `json:"type" jsonschema:"required,enum=gif,enum=image,description=Kind of media to search for."`
	Query   string `json:"query" jsonschema:"required,description=Search query. Supports site: operators for images."`
	Caption string `json:"caption" jsonschema:"description=Optional short text to post alongside the result."`
}

func (w *webTools) mediaSearchTool() llm.Tool {
	return llm.NewTool("web_media", "Search for a GIF or a static image and post it as a response.",
		func(ctx context.Context, in mediaSearchIn) (llm.Result, error) {
			var url string
			var err error
			switch in.Type {
			case "gif":
				url, err = w.giphy.Search(ctx, in.Query)
			case "image":
				url, err = w.brave.SearchImage(ctx, in.Query)
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
			return llm.Reply(llm.RickResponse{Text: text}), nil
		})
}

type webSearchIn struct {
	Query     string `json:"query" jsonschema:"required,description=Search query."`
	Freshness string `json:"freshness" jsonschema:"description=Restrict results by age. Use one of: pd (past 24 hours), pw (past 7 days), pm (past 31 days), py (past year)."`
}

func (w *webTools) webSearchTool() llm.Tool {
	return llm.NewTool("web_search",
		"Search the web and get titles, URLs, and descriptions from the top results. "+
			"Supports standard operators like site:, intitle:, etc. "+
			"Use web_fetch to read the full content of any result URL.",
		func(ctx context.Context, in webSearchIn) (llm.Result, error) {
			result, err := w.brave.Search(ctx, in.Query, in.Freshness)
			if err != nil {
				return llm.Result{}, err
			}
			return llm.Continue(result), nil
		})
}

type fetchPageIn struct {
	URL string `json:"url" jsonschema:"required,description=URL to fetch."`
}

func (w *webTools) fetchPageTool() llm.Tool {
	return llm.NewTool("web_fetch", "Fetch and read the text content of a web page. Use after web_search to dig into a specific result.",
		func(ctx context.Context, in fetchPageIn) (llm.Result, error) {
			content, err := w.brave.FetchPage(ctx, in.URL)
			if err != nil {
				return llm.Result{}, err
			}
			return llm.Continue(content), nil
		})
}
