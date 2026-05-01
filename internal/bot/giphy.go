package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
)

type giphyResponse struct {
	Data []struct {
		Images struct {
			Original struct {
				URL string `json:"url"`
			} `json:"original"`
		} `json:"images"`
	} `json:"data"`
}

func (b *Bot) searchGiphy(ctx context.Context, query string) (string, error) {
	endpoint := fmt.Sprintf(
		"https://api.giphy.com/v1/gifs/search?api_key=%s&q=%s&limit=10&rating=pg-13",
		url.QueryEscape(b.config.GiphyAPIKey),
		url.QueryEscape(query),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("giphy returned %d", resp.StatusCode)
	}

	var gr giphyResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return "", err
	}

	if len(gr.Data) == 0 {
		return "", fmt.Errorf("no results for %q", query)
	}

	pick := gr.Data[rand.Intn(len(gr.Data))]
	return pick.Images.Original.URL, nil
}
