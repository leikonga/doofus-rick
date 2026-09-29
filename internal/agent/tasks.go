package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

const (
	maxTaskResultLen  = 2000
	taskPromptExcerpt = 120
	recentTasksListed = 5
)

type taskStore interface {
	CreateTask(ctx context.Context, t store.Task) (store.Task, error)
	ListTasks(ctx context.Context, recentFinished int) ([]store.Task, error)
	CancelTask(ctx context.Context, id uint64) (store.Task, error)
	ClaimDueTasks(ctx context.Context, now time.Time) ([]store.Task, error)
	FinishTask(ctx context.Context, id uint64, status store.TaskStatus, result string) error
	InterruptRunningTasks(ctx context.Context) ([]store.Task, error)
}

// RunTasks interrupts tasks a previous process left running, then fires due tasks until ctx ends.
func (a *Agent) RunTasks(ctx context.Context) {
	interrupted, err := a.tasks.InterruptRunningTasks(ctx)
	if err != nil {
		slog.Warn("failed to interrupt leftover running tasks", "error", err)
	}
	select {
	case a.interruptedTasks <- interrupted:
	default:
	}

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		a.fireDueTasks(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-a.taskWake:
		}
	}
}

func (a *Agent) wakeTasks() {
	select {
	case a.taskWake <- struct{}{}:
	default:
	}
}

func (a *Agent) fireDueTasks(ctx context.Context) {
	tasks, err := a.tasks.ClaimDueTasks(ctx, time.Now())
	if err != nil {
		slog.Warn("failed to claim due tasks", "error", err)
		return
	}
	for _, task := range tasks {
		labels := pprof.Labels("handler", "task", "task", strconv.FormatUint(task.ID, 10), "channel", strconv.FormatUint(task.ChannelID, 10))
		go pprof.Do(ctx, labels, func(ctx context.Context) { a.runTask(ctx, task) })
	}
}

func (a *Agent) runTask(ctx context.Context, task store.Task) {
	ctx, cancel := context.WithTimeout(ctx, a.turnTimeout)
	a.taskCancels.register(task.ID, cancel)
	defer func() {
		a.taskCancels.forget(task.ID)
		cancel()
	}()

	text, err := a.taskTurn(ctx, task)
	if abandonedByCancelOrShutdown(ctx) {
		return
	}

	status, result := store.TaskDone, text
	if err == nil && text != "" {
		msg := discord.NewMessageCreate().WithContent(taskMessage(snowflake.ID(task.RequesterID), text))
		_, err = a.discordClient.Rest.CreateMessage(snowflake.ID(task.ChannelID), msg)
	}
	if err != nil {
		slog.Warn("task failed", "task", task.ID, "error", err)
		status, result = store.TaskFailed, err.Error()
	}

	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	if err := a.tasks.FinishTask(finishCtx, task.ID, status, capText(result, maxTaskResultLen)); err != nil {
		slog.Warn("failed to record task result", "task", task.ID, "status", status, "error", err)
	}
}

func abandonedByCancelOrShutdown(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

type taskCancels struct {
	byID sync.Map
}

func (c *taskCancels) register(id uint64, cancel context.CancelFunc) {
	c.byID.Store(id, cancel)
}

func (c *taskCancels) forget(id uint64) {
	c.byID.Delete(id)
}

func (c *taskCancels) cancel(id uint64) {
	if cancel, ok := c.byID.LoadAndDelete(id); ok {
		cancel.(context.CancelFunc)()
	}
}

// taskTurn runs one full Rick turn for a due task and returns the text to post, "" on decline.
func (a *Agent) taskTurn(ctx context.Context, task store.Task) (_ string, err error) {
	defer recoverTurn(ctx, &err)

	systemPrompt, err := os.ReadFile(a.config.SystemPromptFile)
	if err != nil {
		return "", err
	}

	channelID := snowflake.ID(task.ChannelID)
	requesterID := snowflake.ID(task.RequesterID)
	msgs, err := a.discordClient.Rest.GetMessages(channelID, 0, 0, 0, historyLimit)
	if err != nil {
		return "", fmt.Errorf("fetch channel history: %w", err)
	}
	slices.Reverse(msgs)
	history := buildHistory(a.discordClient.ID(), 0, msgs, a.memberName)

	triggerLabel := taskTriggerLabel(task, a.userName(requesterID))

	channel := a.channelInfo(channelID)
	recallCh := make(chan string, 1)
	go func() {
		recallCh <- a.buildRecallBlock(ctx, task.Prompt, a.visibleChannelIDs(requesterID))
	}()
	leit, gradDo := a.buildUserRoster(ctx, channel.overwrites)
	recall := <-recallCh

	now := time.Now()
	resp, err := a.callModel(ctx, modelRequest{
		system:      string(systemPrompt) + buildCachedPrefix(a.selbstBlock, leit, channel.id.String(), channel.name, channel.topic),
		messages:    []llm.Message{llm.NewUserMessage(llm.TextPart(buildVolatileTurn(now, a.vitals(now), gradDo, recall, history, triggerLabel)))},
		tracePrompt: triggerLabel,
		origin:      turnOrigin{ChannelID: channelID, AuthorID: requesterID, TaskID: task.ID},
	})
	if err != nil {
		return "", err
	}
	if resp.Decline {
		return "", nil
	}
	return strings.TrimSpace(trailingTagRe.ReplaceAllString(resp.Text, "")), nil
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

// ReportInterruptedTasks posts one persona note per task RunTasks found cut off by the last shutdown.
func (a *Agent) ReportInterruptedTasks(ctx context.Context) {
	var tasks []store.Task
	select {
	case tasks = <-a.interruptedTasks:
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
