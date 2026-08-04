package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/leikonga/doofus-rick/internal/llm"
)

type AffinityScorerConfig struct {
	Model     string
	MaxTokens int64
}

// AffinityScorer judges a closed chunk Rick participated in and nudges each
// other participant's affinity score. Personality-free by design, like the
// ambient classifier: this is a neutral judgment call about how an exchange
// reflects on the relationship, not something Rick's own persona should be
// deciding about itself.
type AffinityScorer struct {
	config   AffinityScorerConfig
	client   *llm.Client
	affinity *Affinity
}

func NewAffinityScorer(config AffinityScorerConfig, c *llm.Client, aff *Affinity) *AffinityScorer {
	if config.MaxTokens == 0 {
		config.MaxTokens = 300
	}
	return &AffinityScorer{config: config, client: c, affinity: aff}
}

type affinityDelta struct {
	UserID uint64 `json:"user_id,string"`
	Delta  int    `json:"delta"`
	Reason string `json:"reason"`
}

type affinityScoreResult struct {
	Users []affinityDelta `json:"users"`
}

const affinityScorerSystemPrompt = "You are a neutral relationship scorer. Output only JSON with a \"users\" array."

// ScoreChunk scores every non-bot participant other than rick in the chunk.
// A no-op if rick isn't one of the chunk's authors, or if nobody else is.
func (s *AffinityScorer) ScoreChunk(ctx context.Context, chunk Chunk, rickID uint64) error {
	rickSpoke := false
	participants := make(map[uint64]struct{})
	for _, m := range chunk.Messages {
		if m.AuthorID == rickID && m.IsBot {
			rickSpoke = true
			continue
		}
		if m.IsBot {
			continue
		}
		participants[m.AuthorID] = struct{}{}
	}
	if !rickSpoke || len(participants) == 0 {
		return nil
	}

	var ids strings.Builder
	for id := range participants {
		if ids.Len() > 0 {
			ids.WriteString(", ")
		}
		fmt.Fprintf(&ids, "%d", id)
	}

	prompt := fmt.Sprintf(`Judge how this conversation burst should move rick's opinion of each of these Discord user IDs: %s.

Good banter, making rick laugh, or genuine kindness moves a user's score up. Being rude to rick, boring, or hostile moves it down. Purely neutral conversation gets a delta of 0 and should be omitted. Only score users from the given ID list who actually participated.

Return ONLY valid JSON: {"users": [{"user_id": "<snowflake as a string>", "delta": <int from -10 to 10>, "reason": "<max 12 words>"}]}.

Conversation:
%s`, ids.String(), chunk.Content)

	resp, err := s.client.Complete(ctx, llm.CompletionRequest{
		Model:     s.config.Model,
		MaxTokens: s.config.MaxTokens,
		System:    affinityScorerSystemPrompt,
		Messages:  []llm.Message{llm.NewUserMessage(llm.TextPart(prompt))},
	})
	if err != nil {
		return err
	}

	var text strings.Builder
	for _, p := range resp.Message.Parts {
		if p.Type == "text" {
			text.WriteString(p.Text)
		}
	}

	var result affinityScoreResult
	if err := json.Unmarshal([]byte(text.String()), &result); err != nil {
		return err
	}

	for _, d := range result.Users {
		if _, ok := participants[d.UserID]; !ok {
			continue // model named someone outside the given participant list; ignore
		}
		delta := clamp(d.Delta, -10, 10)
		if delta == 0 {
			continue
		}
		if err := s.affinity.Update(ctx, d.UserID, d.Reason, delta); err != nil {
			return err
		}
	}
	return nil
}
