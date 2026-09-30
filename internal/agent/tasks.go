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
	"github.com/leikonga/doofus-rick/internal/store"
)

const (
	maxTaskResultLen  = 2000
	taskPromptExcerpt = 120
	recentTasksListed = 5
)

func (a *Agent) postTask(ctx context.Context, channelID snowflake.ID, content string) error {
	msg := discord.NewMessageCreate().WithContent(content)
	_, err := a.discordClient.Rest.CreateMessage(channelID, msg, rest.WithCtx(ctx))
	return err
}

type taskReply struct {
	Text     string
	Declined bool
}

func (a *Agent) taskTurn(ctx context.Context, task store.Task) (_ taskReply, err error) {
	defer recoverTurn(ctx, &err)

	systemPrompt, err := os.ReadFile(a.config.SystemPromptFile)
	if err != nil {
		return taskReply{}, err
	}

	channelID := snowflake.ID(task.ChannelID)
	requesterID := snowflake.ID(task.RequesterID)
	msgs, err := a.discordClient.Rest.GetMessages(channelID, 0, 0, 0, historyLimit, rest.WithCtx(ctx))
	if err != nil {
		return taskReply{}, fmt.Errorf("fetch channel history: %w", err)
	}
	slices.Reverse(msgs)
	history := buildHistory(a.discordClient.ID(), 0, msgs, a.memberName)

	triggerLabel := taskTriggerLabel(task, a.userName(requesterID))

	channel := a.channelInfo(ctx, channelID)
	recallCh := make(chan string, 1)
	go func() {
		recallCh <- a.buildRecallBlock(ctx, task.Prompt, a.visibleChannelIDs(ctx, requesterID))
	}()
	roster := a.buildUserRoster(ctx, channel.overwrites)
	recall := <-recallCh

	now := time.Now()
	resp, err := a.callModel(ctx, modelRequest{
		system:      string(systemPrompt) + buildCachedPrefix(a.selbstBlock, roster.Leit, channel.id.String(), channel.name, channel.topic),
		messages:    []llm.Message{llm.NewUserMessage(llm.TextPart(buildVolatileTurn(now, a.vitals(now), roster.GradDo, recall, history, triggerLabel)))},
		tracePrompt: triggerLabel,
		origin:      turnOrigin{ChannelID: channelID, AuthorID: requesterID, TaskID: task.ID},
	})
	if err != nil {
		return taskReply{}, err
	}
	if resp.Decline {
		return taskReply{Declined: true}, nil
	}
	return taskReply{Text: strings.TrimSpace(trailingTagRe.ReplaceAllString(resp.Text, ""))}, nil
}

func taskTriggerLabel(task store.Task, requesterName string) string {
	return fmt.Sprintf("[task #%d von %s vom %s, jetzt fällig]: %s",
		task.ID, requesterName, task.CreatedAt.Local().Format("2006-01-02 15:04"), task.Prompt)
}

func taskMessage(requester snowflake.ID, text string) string {
	if strings.Contains(text, "<@"+requester.String()+">") || strings.Contains(text, "<@!"+requester.String()+">") {
		return text
	}
	return fmt.Sprintf("<@%s> %s", requester, text)
}

// ReportInterruptedTasks posts one persona note per task the Scheduler found cut off by the last shutdown.
func (a *Agent) ReportInterruptedTasks(ctx context.Context) {
	var tasks []store.Task
	select {
	case tasks = <-a.tasks.interrupted:
	case <-ctx.Done():
		return
	}
	for _, note := range interruptedNotes(tasks) {
		labels := pprof.Labels("handler", "task_interrupted", "channel", note.channelID.String())
		pprof.Do(ctx, labels, func(ctx context.Context) {
			if _, err := a.handleAmbient(ctx, note); err != nil {
				slog.Warn("interrupted task report failed", "channel", note.channelID, "error", err)
			}
		})
	}
}

func interruptedNotes(tasks []store.Task) []personaNote {
	notes := make([]personaNote, 0, len(tasks))
	for _, t := range tasks {
		requester := snowflake.ID(t.RequesterID)
		notes = append(notes, personaNote{
			channelID:   snowflake.ID(t.ChannelID),
			recallQuery: t.Prompt,
			trigger: fmt.Sprintf("[task report, you just restarted]: task #%d that <@%s> gave you (%q) was running when you restarted and got cut off. it will not run again.",
				t.ID, requester, oneLine(t.Prompt, taskPromptExcerpt)),
			traceUser: "task_interrupted",
			mention:   requester,
		})
	}
	return notes
}

func (a *Agent) userName(id snowflake.ID) string {
	name, err := a.discord.GetUsernameForID(id.String())
	if err != nil || name == "" {
		return id.String()
	}
	return name
}

func oneLine(s string, maxLen int) string {
	return capText(strings.Join(strings.Fields(s), " "), maxLen)
}

func capText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return strings.ToValidUTF8(s[:maxLen], "") + "..."
}
