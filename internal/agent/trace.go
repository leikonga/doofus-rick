package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/leikonga/doofus-rick/internal/tracer"
)

func (a *Agent) finishTrace(ctx context.Context, rec *tracer.Recording, text string, decline bool, err error, model string) {
	a.wg.Go(func() {
		e := rec.Finish(text, decline, err)
		// Usage records work already done, so it must survive shutdown cancellation.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		usage := store.TokenUsage{ChannelID: e.ChannelID, UserID: e.UserID, ModelName: model, InputTokens: e.InputTokens, OutputTokens: e.OutputTokens}
		if err := a.store.SaveTokenUsage(saveCtx, usage); err != nil {
			slog.Warn("failed to save token usage", "error", err)
		}
	})
}
