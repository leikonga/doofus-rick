package client

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

type GiphyClient struct {
	http   *http.Client
	apiKey string
}

func NewGiphy(http *http.Client, apiKey string) *GiphyClient {
	return &GiphyClient{http: http, apiKey: apiKey}
}

func (c *GiphyClient) Search(ctx context.Context, query string) (string, error) {
	params := url.Values{}
	params.Set("api_key", c.apiKey)
	params.Set("q", query)
	params.Set("limit", "10")
	params.Set("rating", "pg-13")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.giphy.com/v1/gifs/search?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}

	resp, err := c.http.Do(req)
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
