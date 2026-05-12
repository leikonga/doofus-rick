package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
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

func (b *Bot) searchBrave(ctx context.Context, query string) (string, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("maximum_number_of_tokens", "2000")
	params.Set("maximum_number_of_urls", "3")

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
