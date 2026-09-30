package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/pprof"
	"slices"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/runtimehome"
	"github.com/leikonga/doofus-rick/internal/selbst"
)

// HandleAmbient runs an unprompted, reduced persona call for a burst the
// ambient classifier flagged, and posts it as a plain (non-reply) message.
// Per the plan, an ambient interjection is a one-liner, not the start of an
// agentic tool loop, so unlike handleMention this makes a single completion
// call with no tools. Returns the sent message's ID (0 if nothing was sent).
func (a *Agent) HandleAmbient(ctx context.Context, channelID snowflake.ID, hook string) (sentID snowflake.ID, err error) {
	labels := pprof.Labels("handler", "ambient", "channel", channelID.String())
	pprof.Do(ctx, labels, func(ctx context.Context) {
		sentID, err = a.handleAmbient(ctx, personaNote{
			channelID:   channelID,
			recallQuery: hook,
			trigger:     fmt.Sprintf("[ambient hook, no one asked, do not reply to any single message]: %s", hook),
			traceUser:   "ambient",
		})
	})
	return sentID, err
}

func (a *Agent) ReportDeploy(ctx context.Context) {
	if a.deploys == nil {
		slog.Debug("deploy journal unavailable, skipping deploy report")
		return
	}
	records, err := a.deploys.Records()
	if err != nil {
		slog.Warn("failed to read deploy journal", "error", err)
		return
	}
	var crashes []runtimehome.CrashReport
	if a.runtimeLogs != nil {
		if crashes, err = a.runtimeLogs.CrashReports(); err != nil {
			slog.Warn("failed to list crash reports for deploy report", "error", err)
		}
	}
	ann, ok := selbst.EvaluateBoot(records, selbst.Commit(), crashes, a.crashFile)
	if !ok {
		return
	}
	channelID, err := snowflake.Parse(ann.Ship.ChannelID)
	if err != nil {
		slog.Warn("deploy journal ship has no usable channel", "channel_id", ann.Ship.ChannelID, "error", err)
		return
	}
	var excerpt string
	if ann.CrashFile != "" {
		if excerpt, err = selbst.ReadCrashExcerpt(ann.CrashFile); err != nil {
			slog.Warn("failed to read crash excerpt", "file", ann.CrashFile, "error", err)
		}
	}
	// Recorded before sending so a crash while announcing cannot repeat the announcement next boot.
	if err := a.deploys.Append(ann.Record(time.Now())); err != nil {
		slog.Warn("failed to record deploy report, skipping it", "error", err)
		return
	}
	mention, _ := snowflake.Parse(ann.Ship.Requester)
	labels := pprof.Labels("handler", "deploy_report", "channel", channelID.String())
	pprof.Do(ctx, labels, func(ctx context.Context) {
		if _, err := a.handleAmbient(ctx, personaNote{
			channelID:   channelID,
			recallQuery: ann.Ship.Summary,
			trigger:     fmt.Sprintf("[deploy report, you just restarted after your own code_ship]: %s", ann.Facts(excerpt)),
			traceUser:   "deploy_report",
			mention:     mention,
			appendix:    codeBlock(excerpt),
		}); err != nil {
			slog.Warn("deploy report failed", "channel", channelID, "outcome", ann.Outcome, "error", err)
		}
	})
}

type personaNote struct {
	channelID   snowflake.ID
	recallQuery string
	trigger     string
	traceUser   string
	mention     snowflake.ID
	appendix    string
}

func (a *Agent) handleAmbient(ctx context.Context, note personaNote) (_ snowflake.ID, err error) {
	defer recoverTurn(ctx, &err)
	channelID := note.channelID

	systemPrompt, err := os.ReadFile(a.config.SystemPromptFile)
	if err != nil {
		return 0, err
	}

	botID := a.discordClient.ID()
	msgs, err := a.discordClient.Rest.GetMessages(channelID, 0, 0, 0, historyLimit, rest.WithCtx(ctx))
	if err != nil {
		return 0, err
	}
	slices.Reverse(msgs)
	history := buildHistory(botID, 0, msgs, a.memberName)

	channel := a.channelInfo(ctx, channelID)
	leit, gradDo := a.buildUserRoster(ctx, channel.overwrites)
	recall := a.buildRecallBlock(ctx, note.recallQuery, []uint64{uint64(channelID)})
	system := string(systemPrompt) + buildCachedPrefix(a.selbstBlock, leit, channel.id.String(), channel.name, channel.topic)
	now := time.Now()
	turn := buildVolatileTurn(now, a.vitals(now), gradDo, recall, history, note.trigger)

	rec := a.tracer.Start(channelID.String(), note.traceUser, system, note.trigger)
	resp, err := a.llm.Complete(ctx, llm.CompletionRequest{
		Model:           a.config.RickModel,
		MaxTokens:       a.config.AmbientMaxTokens,
		ReasoningEffort: a.config.RickReasoningEffort,
		SessionID:       channelID.String(),
		System:          system,
		Messages:        []llm.Message{llm.NewUserMessage(llm.TextPart(turn))},
	})
	if err == nil {
		rec.AddTokens(resp.InputTokens, resp.OutputTokens)
	}
	var rawText string
	servedModel := a.config.RickModel
	if err == nil {
		rawText = resp.Message.Text()
		if resp.Model != "" {
			servedModel = resp.Model
		}
	}
	a.finishTrace(ctx, rec, rawText, false, err, servedModel)
	if err != nil {
		return 0, err
	}

	text := strings.TrimSpace(trailingTagRe.ReplaceAllString(resp.Message.Text(), ""))
	if text == "" && note.appendix == "" {
		return 0, nil
	}
	if note.mention != 0 && !strings.Contains(text, note.mention.String()) {
		text = strings.TrimSpace(fmt.Sprintf("<@%s> %s", note.mention, text))
	}

	var firstID snowflake.ID
	for _, content := range []string{text, note.appendix} {
		if content == "" {
			continue
		}
		sent, err := a.discordClient.Rest.CreateMessage(channelID, discord.NewMessageCreate().WithContent(content), rest.WithCtx(ctx))
		if err != nil {
			return firstID, err
		}
		if firstID == 0 {
			firstID = sent.ID
		}
	}
	return firstID, nil
}

func codeBlock(s string) string {
	if s == "" {
		return ""
	}
	return "```\n" + s + "\n```"
}
