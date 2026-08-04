package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

func (a *Agent) discordTools() llm.Tools {
	return llm.Tools{
		a.sendMessageTool(),
		a.createPollTool(),
		a.sendFileTool(),
		a.scheduleReminderTool(),
	}
}

type sendMessageIn struct {
	ChannelID string `json:"channel_id" jsonschema:"required,description=Discord channel snowflake ID to send the message to."`
	Content   string `json:"content" jsonschema:"required,description=Message content to send."`
}

func (a *Agent) sendMessageTool() llm.Tool {
	return llm.NewTool("send_message", "Send a message to any channel by ID. Use this to post in a different channel than the one you were mentioned in.",
		func(_ context.Context, in sendMessageIn) (llm.Result, error) {
			chID, err := snowflake.Parse(in.ChannelID)
			if err != nil {
				return llm.Result{}, err
			}
			if _, err := a.discordClient.Rest.CreateMessage(chID, discord.NewMessageCreate().WithContent(in.Content)); err != nil {
				return llm.Result{}, err
			}
			return llm.Result{Content: "message sent", Done: true}, nil
		})
}

type createPollIn struct {
	ChannelID        string   `json:"channel_id" jsonschema:"required,description=Discord channel snowflake ID."`
	Question         string   `json:"question" jsonschema:"required,description=Poll question text."`
	Answers          []string `json:"answers" jsonschema:"required,description=List of answer options."`
	DurationHours    int      `json:"duration_hours" jsonschema:"required,description=Poll duration in hours (1-168)."`
	AllowMultiselect bool     `json:"allow_multiselect" jsonschema:"description=Whether multiple answers can be selected."`
}

func (a *Agent) createPollTool() llm.Tool {
	return llm.NewTool("create_poll", "Create a Discord poll in a channel.",
		func(_ context.Context, in createPollIn) (llm.Result, error) {
			chID, err := snowflake.Parse(in.ChannelID)
			if err != nil {
				return llm.Result{}, err
			}
			poll := discord.NewPollCreate(in.Question)
			for _, ans := range in.Answers {
				poll = poll.AddAnswer(ans, nil)
			}
			poll = poll.WithDuration(in.DurationHours).WithAllowMultiselect(in.AllowMultiselect)
			if _, err := a.discordClient.Rest.CreateMessage(chID, discord.NewMessageCreate().WithPoll(poll)); err != nil {
				return llm.Result{}, err
			}
			return llm.Result{Content: "poll created", Done: true}, nil
		})
}

type sendFileIn struct {
	ChannelID string `json:"channel_id" jsonschema:"required,description=Discord channel snowflake ID to post the file in."`
	Path      string `json:"path" jsonschema:"required,description=Absolute path to the file inside /rick/work, e.g. /rick/work/output.txt."`
	Caption   string `json:"caption" jsonschema:"description=Optional message to accompany the file."`
}

func (a *Agent) sendFileTool() llm.Tool {
	return llm.NewTool("send_file",
		"Attach and send a file from the work directory (/rick/work) to any channel. "+
			"Use after shell_exec writes output to a file when the content is too long for a message.",
		func(_ context.Context, in sendFileIn) (llm.Result, error) {
			clean := filepath.Clean(in.Path)
			if !strings.HasPrefix(clean, a.config.WorkDir) {
				slog.Warn("send_file rejected path outside workdir", "path", clean, "workdir", a.config.WorkDir)
				return llm.Result{Content: "path must be inside " + a.config.WorkDir}, nil
			}

			f, err := os.Open(clean)
			if err != nil {
				return llm.Result{}, err
			}
			defer func() {
				if cerr := f.Close(); cerr != nil {
					slog.Warn("failed to close file after send", "error", cerr)
				}
			}()

			msg := discord.NewMessageCreate().AddFile(filepath.Base(clean), "", f)
			if in.Caption != "" {
				msg = msg.WithContent(in.Caption)
			}

			chID, err := snowflake.Parse(in.ChannelID)
			if err != nil {
				return llm.Result{}, err
			}
			if _, err := a.discordClient.Rest.CreateMessage(chID, msg); err != nil {
				return llm.Result{}, err
			}
			return llm.Result{Content: "file sent", Done: true}, nil
		})
}

type scheduleReminderIn struct {
	ChannelID string `json:"channel_id" jsonschema:"required,description=Channel to post the reminder in."`
	UserID    string `json:"user_id" jsonschema:"required,description=Discord snowflake of the user to remind."`
	Message   string `json:"message" jsonschema:"required,description=Reminder message text."`
	FireAt    string `json:"fire_at" jsonschema:"required,description=ISO 8601 UTC timestamp when to fire the reminder, e.g. 2006-01-02T15:04:05Z."`
}

func (a *Agent) scheduleReminderTool() llm.Tool {
	return llm.NewTool("schedule_reminder",
		"Schedule a one-shot reminder that will be posted in a channel at a specific time. "+
			"Use when a user asks to be reminded about something later. "+
			"The reminder will mention the target user.",
		func(ctx context.Context, in scheduleReminderIn) (llm.Result, error) {
			fireAt, err := time.Parse(time.RFC3339, in.FireAt)
			if err != nil {
				return llm.Result{Content: "invalid fire_at format, use ISO 8601 e.g. 2006-01-02T15:04:05Z"}, nil
			}
			if fireAt.Before(time.Now()) {
				return llm.Result{Content: "fire_at is in the past"}, nil
			}
			r := store.Reminder{
				ChannelID: in.ChannelID,
				UserID:    in.UserID,
				Message:   in.Message,
				FireAt:    fireAt,
			}
			if err := a.store.CreateReminder(ctx, r); err != nil {
				return llm.Result{}, err
			}
			return llm.Result{Content: fmt.Sprintf("reminder scheduled for %s", fireAt.Format(time.RFC3339))}, nil
		})
}
