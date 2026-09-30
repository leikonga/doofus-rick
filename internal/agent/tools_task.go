package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

var (
	errTaskFromTask   = errors.New("a task cannot create tasks; tasks run once, do the work in this turn instead")
	errTaskPromptless = errors.New("prompt is required for create")
	errTaskIDMissing  = errors.New("id is required for cancel")
)

type taskIn struct {
	Command string `json:"command" jsonschema:"required,enum=create,enum=list,enum=cancel,description=create schedules a task; list shows everyone's tasks; cancel stops one by id."`
	Prompt  string `json:"prompt" jsonschema:"description=create: what you should do when the task fires, written for your future self who will not see this conversation. Say who asked, what exactly to do and how to report back."`
	FireAt  string `json:"fire_at" jsonschema:"description=create: when to fire, RFC3339 with offset, e.g. 2026-09-29T20:00:00+02:00. Use either fire_at or in; neither means now."`
	In      string `json:"in" jsonschema:"description=create: delay from now as a Go duration, e.g. 45m or 2h30m or 48h (no days unit). Use either fire_at or in; neither means now."`
	ID      uint64 `json:"id" jsonschema:"description=cancel: id of the task to cancel."`
}

func (a *Agent) taskTool(origin turnOrigin) llm.Tool {
	return llm.NewTool("sys_task",
		"Schedule yourself to do something once, later or right now in the background. "+
			"When a task fires it runs a full turn of yours with every tool, in the channel it was created in, "+
			"and your answer is posted there mentioning whoever asked. Each task runs exactly once; a task turn cannot create further tasks. "+
			"Examples: a reminder (\"remind me at 8 to drink water\": prompt tells your future self to remind them in your own voice), "+
			"research and report back (\"find out X and tell me\": fire now, do the digging in the task), "+
			"check something later (\"in 2h check if the site is up\"). "+
			"list shows everyone's pending and running tasks plus the last few finished ones; cancel stops anyone's task by id.",
		func(ctx context.Context, in taskIn) (llm.Result, error) {
			switch in.Command {
			case "create":
				return a.createTask(ctx, origin, in, time.Now())
			case "list":
				return a.listTasks(ctx)
			case "cancel":
				return a.cancelTask(ctx, in.ID)
			default:
				return llm.Result{}, fmt.Errorf("unknown command %q, must be create, list or cancel", in.Command)
			}
		})
}

func (a *Agent) createTask(ctx context.Context, origin turnOrigin, in taskIn, now time.Time) (llm.Result, error) {
	if origin.TaskID != 0 {
		return llm.Result{}, errTaskFromTask
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return llm.Result{}, errTaskPromptless
	}
	fireAt, err := taskFireAt(now, in.FireAt, in.In)
	if err != nil {
		return llm.Result{}, err
	}
	task, err := a.tasks.CreateTask(ctx, store.Task{
		ChannelID:   uint64(origin.ChannelID),
		RequesterID: uint64(origin.AuthorID),
		Prompt:      in.Prompt,
		FireAt:      fireAt,
		CreatedAt:   now,
	})
	if err != nil {
		return llm.Result{}, err
	}
	if wait := fireAt.Sub(now); wait < time.Minute {
		time.AfterFunc(max(wait, 0), a.wakeTasks)
	}
	return llm.Continue(fmt.Sprintf("task #%d created, due %s", task.ID, fireAt.Local().Format(time.RFC3339))), nil
}

func taskFireAt(now time.Time, fireAt, in string) (time.Time, error) {
	switch {
	case fireAt != "" && in != "":
		return time.Time{}, errors.New("set either fire_at or in, not both")
	case fireAt != "":
		t, err := time.Parse(time.RFC3339, fireAt)
		if err != nil {
			return time.Time{}, fmt.Errorf("fire_at must be RFC3339 with offset, e.g. 2026-09-29T20:00:00+02:00: %w", err)
		}
		if t.Before(now) {
			return time.Time{}, fmt.Errorf("fire_at %s is in the past, it is now %s", t.Format(time.RFC3339), now.Local().Format(time.RFC3339))
		}
		return t, nil
	case in != "":
		d, err := time.ParseDuration(in)
		if err != nil {
			return time.Time{}, fmt.Errorf("in must be a Go duration like 45m or 2h30m: %w", err)
		}
		if d < 0 {
			return time.Time{}, fmt.Errorf("in must not be negative, got %s", in)
		}
		return now.Add(d), nil
	default:
		return now, nil
	}
}

func (a *Agent) listTasks(ctx context.Context) (llm.Result, error) {
	tasks, err := a.tasks.ListTasks(ctx, recentTasksListed)
	if err != nil {
		return llm.Result{}, err
	}
	if len(tasks) == 0 {
		return llm.Continue("no tasks"), nil
	}
	var sb strings.Builder
	for _, t := range tasks {
		fmt.Fprintf(&sb, "#%d status=%s due=%s requester=%s (<@%d>) prompt=%q\n",
			t.ID, t.Status, t.FireAt.Local().Format("2006-01-02 15:04"), a.userName(snowflake.ID(t.RequesterID)), t.RequesterID,
			oneLine(t.Prompt, taskPromptExcerpt))
	}
	return llm.Continue(sb.String()), nil
}

func (a *Agent) cancelTask(ctx context.Context, id uint64) (llm.Result, error) {
	if id == 0 {
		return llm.Result{}, errTaskIDMissing
	}
	before, err := a.tasks.CancelTask(ctx, id)
	if err != nil {
		return llm.Result{}, fmt.Errorf("task #%d: %w", id, err)
	}
	a.taskCancels.cancel(id)
	return llm.Continue(fmt.Sprintf("task #%d cancelled (was %s)", id, before.Status)), nil
}
