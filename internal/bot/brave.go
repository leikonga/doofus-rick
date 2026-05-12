package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type braveContextResponse struct {
	Grounding struct {
		Generic []struct {
			URL      string   `json:"url"`
			Title    string   `json:"title"`
			Snippets []string `json:"snippets"`
		} `json:"generic"`
	} `json:"grounding"`
}

type braveImageResponse struct {
	Results []struct {
		Title     string `json:"title"`
		URL       string `json:"url"`
		Thumbnail struct {
			Src string `json:"src"`
		} `json:"thumbnail"`
		Properties struct {
			URL     string `json:"url"`
			Resized string `json:"resized"`
		} `json:"properties"`
	} `json:"results"`
}

var (
	htmlTagRe      = regexp.MustCompile(`<[^>]+>`)
	htmlEntityRe   = regexp.MustCompile(`&[a-zA-Z]+;|&#[0-9]+;`)
	whitespaceRe   = regexp.MustCompile(`[ \t]+`)
	multiNewlineRe = regexp.MustCompile(`\n{3,}`)
)

func (b *Bot) searchBrave(ctx context.Context, query, freshness string) (string, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("maximum_number_of_tokens", "4000")
	params.Set("maximum_number_of_urls", "5")
	if freshness != "" {
		params.Set("freshness", freshness)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.search.brave.com/res/v1/llm/context?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.config.BraveAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("brave search returned %d", resp.StatusCode)
	}

	var br braveContextResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		return "", err
	}

	if len(br.Grounding.Generic) == 0 {
		return "no results found", nil
	}

	var sb strings.Builder
	for _, item := range br.Grounding.Generic {
		fmt.Fprintf(&sb, "[%s](%s)\n", item.Title, item.URL)
		for _, snippet := range item.Snippets {
			sb.WriteString(snippet)
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	return sb.String(), nil
}

func (b *Bot) fetchPage(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; bot)")
	req.Header.Set("Accept", "text/html,text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch returned %d", resp.StatusCode)
	}

	const maxBytes = 128 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", err
	}

	text := string(body)
	text = htmlTagRe.ReplaceAllString(text, " ")
	text = htmlEntityRe.ReplaceAllStringFunc(text, func(s string) string {
		switch s {
		case "&amp;":
			return "&"
		case "&lt;":
			return "<"
		case "&gt;":
			return ">"
		case "&quot;":
			return "\""
		case "&apos;", "&#39;":
			return "'"
		case "&nbsp;":
			return " "
		default:
			return " "
		}
	})
	text = whitespaceRe.ReplaceAllString(text, " ")

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	text = strings.Join(lines, "\n")
	text = multiNewlineRe.ReplaceAllString(text, "\n\n")

	const maxChars = 8000
	if len(text) > maxChars {
		text = text[:maxChars] + "\n[truncated]"
	}

	return text, nil
}

func (b *Bot) searchBraveImage(ctx context.Context, query string) (string, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("count", "10")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.search.brave.com/res/v1/images/search?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.config.BraveAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("brave image search returned %d", resp.StatusCode)
	}

	var br braveImageResponse
	if err := json.NewDecoder(resp.Body).Decode(&br); err != nil {
		return "", err
	}

	if len(br.Results) == 0 {
		return "", fmt.Errorf("no image results for %q", query)
	}

	pick := br.Results[rand.Intn(min(len(br.Results), 5))]
	if pick.Properties.Resized != "" {
		return pick.Properties.Resized, nil
	}
	if pick.Properties.URL != "" {
		return pick.Properties.URL, nil
	}
	if pick.Thumbnail.Src != "" {
		return pick.Thumbnail.Src, nil
	}
	return pick.URL, nil
}
