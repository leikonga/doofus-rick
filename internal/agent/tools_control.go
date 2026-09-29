package agent

import (
	"context"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
)

type turnOrigin struct {
	ChannelID snowflake.ID
	AuthorID  snowflake.ID
	MessageID snowflake.ID
	TaskID    uint64
}

func (a *Agent) buildTools(origin turnOrigin) llm.Tools {
	tools := llm.Tools{
		a.declineTool(),
		a.checkLogsTool(),
		a.mediaSearchTool(),
		a.webSearchTool(),
		a.fetchPageTool(),
		a.shellExecTool(),
		a.saveQuoteTool(origin),
		a.getUserQuotesTool(),
		a.searchHistoryTool(origin),
		a.codeReadTool(),
		a.codeEditTool(),
		a.codeShipTool(origin),
		a.taskTool(origin),
	}
	return append(tools, a.discordTools(origin)...)
}

type declineIn struct {
	Emoji string `json:"emoji" jsonschema:"description=Unicode emoji to react with."`
}

func (a *Agent) declineTool() llm.Tool {
	return llm.NewTool("decline", "Decline to respond and optionally react with an emoji instead.",
		func(_ context.Context, in declineIn) (llm.Result, error) {
			return llm.Result{Response: &llm.RickResponse{Decline: true, Emoji: in.Emoji}}, nil
		})
}
