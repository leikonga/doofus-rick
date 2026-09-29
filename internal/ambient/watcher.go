package ambient

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

type Responder interface {
	HandleAmbient(ctx context.Context, channelID snowflake.ID, hook string) (snowflake.ID, error)
}

type classifier interface {
	Classify(ctx context.Context, channelID uint64, messages []llm.Message) (ClassifierResult, error)
}

type WatcherConfig struct {
	Enabled bool
	Window  time.Duration
}

type Watcher struct {
	config     WatcherConfig
	store      *store.Store
	gate       *Gate
	classifier classifier
	responder  Responder
	selfID     func() snowflake.ID
}

func NewWatcher(cfg WatcherConfig, s *store.Store, gate *Gate, c classifier, r Responder, selfID func() snowflake.ID) *Watcher {
	return &Watcher{config: cfg, store: s, gate: gate, classifier: c, responder: r, selfID: selfID}
}

// Check evaluates the ambient gate for a channel after a human message lands,
// and fires an unprompted response if it passes. Runs in its own goroutine so
// it never delays message handling.
func (w *Watcher) Check(channelID snowflake.ID) {
	if !w.config.Enabled || w.gate == nil || w.classifier == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		w.check(ctx, channelID)
	}()
}

func (w *Watcher) check(ctx context.Context, channelID snowflake.ID) {
	since := time.Now().Add(-w.config.Window)
	msgs, err := w.store.GetRecentMessagesSince(ctx, uint64(channelID), since, 200)
	if err != nil {
		slog.Warn("failed to load ambient window", "channel", channelID, "error", err)
		return
	}

	result := w.gate.CheckGate(ctx, channelID, w.selfID(), msgs)
	if err := w.gate.EvalTouch(ctx, channelID); err != nil {
		slog.Warn("failed to record ambient eval", "channel", channelID, "error", err)
	}
	if !result.Passed {
		return
	}

	llmMsgs := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		name := m.AuthorName
		if m.IsBot {
			name += " (bot)"
		}
		llmMsgs = append(llmMsgs, llm.NewUserMessage(llm.TextPart(fmt.Sprintf("[%s]: %s", name, m.Content))))
	}

	classified, err := w.classifier.Classify(ctx, uint64(channelID), llmMsgs)
	if err != nil {
		slog.Warn("ambient classification failed", "channel", channelID, "error", err)
		return
	}
	if classified.Hook == "" {
		return
	}

	sentID, err := w.responder.HandleAmbient(ctx, channelID, classified.Hook)
	if err != nil {
		slog.Warn("ambient response failed", "channel", channelID, "error", err)
		return
	}

	if err := w.gate.LogFire(ctx, channelID, classified.Score, classified.Hook); err != nil {
		slog.Warn("failed to log ambient fire", "channel", channelID, "error", err)
	}
	if err := w.gate.UpdateState(ctx, channelID, uint64(sentID)); err != nil {
		slog.Warn("failed to update ambient state", "channel", channelID, "error", err)
	}
}
