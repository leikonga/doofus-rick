package agent

import (
	"context"

	"github.com/disgoorg/disgo/events"
	"github.com/leikonga/doofus-rick/internal/llm"
)

func (a *Agent) buildTools(event *events.MessageCreate) llm.Tools {
	tools := llm.Tools{
		a.declineTool(),
		a.checkLogsTool(),
		a.mediaSearchTool(),
		a.webSearchTool(),
		a.fetchPageTool(),
		a.shellExecTool(),
		a.saveQuoteTool(event),
		a.getUserQuotesTool(),
		a.searchHistoryTool(event),
		a.codeReadTool(),
		a.codeEditTool(),
	}
	return append(tools, a.discordTools(event)...)
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
