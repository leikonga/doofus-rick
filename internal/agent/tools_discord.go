package agent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
)

func (a *Agent) discordTools(origin turnOrigin) llm.Tools {
	return llm.Tools{
		a.sendMessageTool(),
		a.createPollTool(),
		a.sendFileTool(),
		a.reactTool(origin),
	}
}

type sendMessageIn struct {
	ChannelID string `json:"channel_id" jsonschema:"required,description=Discord channel snowflake ID to send the message to."`
	Content   string `json:"content" jsonschema:"required,description=Message content to send."`
}

func (a *Agent) sendMessageTool() llm.Tool {
	return llm.NewTool("discord_send_message", "Send a message to any channel by ID. Use this to post in a different channel than the one you were mentioned in.",
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
	return llm.NewTool("discord_create_poll", "Create a Discord poll in a channel.",
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
	return llm.NewTool("discord_send_file",
		"Attach and send a file from the work directory (/rick/work) to any channel. "+
			"Use after sys_shell writes output to a file when the content is too long for a message.",
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

type reactIn struct {
	Emojis []string `json:"emojis" jsonschema:"required,description=Unicode emojis to react with."`
}

var errNoMessageToReact = errors.New("no message to react to in a task")

func (a *Agent) reactTool(origin turnOrigin) llm.Tool {
	return llm.NewTool("discord_react", "Add one or more emoji reactions to the message you are replying to. Can be used alongside a text response.",
		func(_ context.Context, in reactIn) (llm.Result, error) {
			if origin.MessageID == 0 {
				return llm.Result{}, errNoMessageToReact
			}
			for _, emoji := range in.Emojis {
				if err := a.discordClient.Rest.AddReaction(origin.ChannelID, origin.MessageID, emoji); err != nil {
					slog.Warn("failed to add reaction", "emoji", emoji, "error", err)
				}
			}
			return llm.Result{Content: "reactions added"}, nil
		})
}
