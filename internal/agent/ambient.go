package agent

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
)

// HandleAmbient runs an unprompted, reduced persona call for a burst the
// ambient classifier flagged, and posts it as a plain (non-reply) message.
// Per the plan, an ambient interjection is a one-liner, not the start of an
// agentic tool loop, so unlike handleMention this makes a single completion
// call with no tools. Returns the sent message's ID (0 if nothing was sent).
func (a *Agent) HandleAmbient(ctx context.Context, channelID snowflake.ID, hook string) (snowflake.ID, error) {
	systemPrompt, err := os.ReadFile(a.config.SystemPromptFile)
	if err != nil {
		return 0, err
	}

	botID := a.discordClient.ID()
	msgs, err := a.discordClient.Rest.GetMessages(channelID, 0, 0, 0, historyLimit)
	if err != nil {
		return 0, err
	}
	slices.Reverse(msgs)
	messages := buildTranscript(botID, 0, msgs, a.memberName)

	var channelName, channelTopic string
	var channelOverwrites discord.PermissionOverwrites
	if ch, err := a.discordClient.Rest.GetChannel(channelID); err == nil {
		channelName = ch.Name()
		if gmc, ok := ch.(discord.GuildMessageChannel); ok {
			if gmc.Topic() != nil {
				channelTopic = *gmc.Topic()
			}
			channelOverwrites = gmc.PermissionOverwrites()
		}
	}

	leit, gradDo := a.buildUserRoster(ctx, channelOverwrites)
	recall := a.buildRecallBlock(ctx, hook, []uint64{uint64(channelID)})
	hookLabel := fmt.Sprintf("[ambient hook, no one asked, do not reply to any single message]: %s", hook)
	if len(messages) > 0 {
		messages = append(messages, checkpointMessage())
	}
	messages = append(messages, llm.NewUserMessage(llm.TextPart(hookLabel)))

	systemFull := string(systemPrompt) + buildCachedPrefix(leit, channelName, channelTopic)
	if tail := buildUncachedTail(gradDo, recall); tail != "" {
		systemFull += "\n\n" + tail
	}

	rec := a.tracer.Start(channelID.String(), "ambient", systemFull, hookLabel)
	resp, err := a.llm.Complete(ctx, llm.CompletionRequest{
		Model:     a.config.RickModel,
		MaxTokens: a.config.AmbientMaxTokens,
		System:    systemFull,
		Messages:  messages,
	})
	if err == nil {
		rec.AddTokens(resp.InputTokens, resp.OutputTokens)
	}
	go func() {
		var text string
		if err == nil {
			text = messageText(resp.Message)
		}
		e := rec.Finish(text, false, err)
		if e.InputTokens > 0 || e.OutputTokens > 0 {
			saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			a.store.SaveTokenUsage(saveCtx, e.ChannelID, e.UserID, a.config.RickModel, e.InputTokens, e.OutputTokens)
		}
	}()
	if err != nil {
		return 0, err
	}

	text := strings.TrimSpace(trailingTagRe.ReplaceAllString(messageText(resp.Message), ""))
	if text == "" {
		return 0, nil
	}

	sent, err := a.discordClient.Rest.CreateMessage(channelID, discord.NewMessageCreate().WithContent(text))
	if err != nil {
		return 0, err
	}
	return sent.ID, nil
}
