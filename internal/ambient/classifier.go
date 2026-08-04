package ambient

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/leikonga/doofus-rick/internal/llm"
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
}

func NewClassifier(config ClassifierConfig, apiKey string) *Classifier {
	if config.MinScore == 0 {
		config.MinScore = 90
	}
	return &Classifier{
		config: config,
		client: llm.NewClient(apiKey),
	}
}

func (c *Classifier) Classify(ctx context.Context, messages []llm.Message) (ClassifierResult, error) {
	if len(messages) == 0 {
		return ClassifierResult{}, nil
	}

	var content string
	for i, msg := range messages {
		if i >= 10 {
			break
		}
		for _, part := range msg.Parts {
			if part.Type == "text" {
				content += fmt.Sprintf("[%s]: %s\n", msg.Role, part.Text)
			}
		}
	}

	prompt := `Classify the following Discord conversation for potential roast material. Return a JSON object with:
- "score": 0-100, where 100 is the funniest/most roast-worthy
- "hook": a short phrase (max 10 words) that captures the roast opportunity, or empty if none

Return ONLY valid JSON, no other text.

Conversation:
` + content

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

	var result ClassifierResult
	if err := json.Unmarshal([]byte(resp.Message.Parts[0].Text), &result); err != nil {
		return ClassifierResult{}, err
	}

	if result.Hook == "" || result.Score < c.config.MinScore {
		return ClassifierResult{}, nil
	}

	return result, nil
}
