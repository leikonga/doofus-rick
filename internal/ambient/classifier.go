package ambient

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

type ClassifierConfig struct {
	Model     string
	MaxTokens int64
	MinScore  int
}

type ClassifierResult struct {
	Score int
	Hook  string
}

type Classifier struct {
	config ClassifierConfig
	client *llm.Client
	store  *store.Store
}

func NewClassifier(config ClassifierConfig, c *llm.Client, s *store.Store) *Classifier {
	if config.MinScore == 0 {
		config.MinScore = 90
	}
	return &Classifier{config: config, client: c, store: s}
}

func (c *Classifier) Classify(ctx context.Context, channelID uint64, messages []llm.Message) (ClassifierResult, error) {
	if len(messages) == 0 {
		return ClassifierResult{}, nil
	}

	var content strings.Builder
	for i, msg := range messages {
		if i >= 10 {
			break
		}
		for _, part := range msg.Parts {
			if part.Type == "text" {
				fmt.Fprintf(&content, "[%s]: %s\n", msg.Role, part.Text)
			}
		}
	}

	prompt := `Classify the following Discord conversation for potential roast material. Return a JSON object with:
- "score": 0-100, where 100 is the funniest/most roast-worthy
- "hook": a short phrase (max 10 words) that captures the roast opportunity, or empty if none

Return ONLY valid JSON, no other text.

Conversation:
` + content.String()

	req := llm.CompletionRequest{
		Model:     c.config.Model,
		MaxTokens: c.config.MaxTokens,
		System:    "You are a roast classifier. Output only JSON with score and hook.",
		Messages: []llm.Message{
			llm.NewUserMessage(llm.TextPart(prompt)),
		},
	}

	resp, err := c.client.Complete(ctx, req)
	if err != nil {
		return ClassifierResult{}, err
	}
	c.store.SaveTokenUsage(ctx, strconv.FormatUint(channelID, 10), "ambient-classifier", c.config.Model, resp.InputTokens, resp.OutputTokens)

	var result ClassifierResult
	if err := json.Unmarshal([]byte(resp.Message.Parts[0].Text), &result); err != nil {
		return ClassifierResult{}, err
	}

	if result.Hook == "" || result.Score < c.config.MinScore {
		return ClassifierResult{}, nil
	}

	return result, nil
}
