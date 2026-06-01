package agent

import (
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
)

// validateConversation checks that the message slice is sendable and repairs
// it by dropping leading orphan messages (e.g. a tool_result with no prior
// tool_use). Logs at warn with the offending slice on repair because a firing
// repair indicates a real assembly regression. Returns the repaired slice and
// whether repair occurred. An empty result means nothing valid remains.
func validateConversation(messages []anthropic.MessageParam) ([]anthropic.MessageParam, bool) {
	if len(messages) == 0 || isUserTurnStart(messages[0]) {
		return messages, false
	}
	slog.Warn("conversation repair: dropping leading orphan(s)", "messages", messages)
	for len(messages) > 0 && !isUserTurnStart(messages[0]) {
		messages = messages[1:]
	}
	return messages, true
}
