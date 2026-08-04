package agent

import (
	"log/slog"

	"github.com/leikonga/doofus-rick/internal/llm"
)

// validateConversation drops leading orphan messages (e.g. a tool message
// with no prior tool call) and reports whether it had to.
func validateConversation(messages []llm.Message) ([]llm.Message, bool) {
	if len(messages) == 0 || isUserTurnStart(messages[0]) {
		return messages, false
	}
	slog.Warn("conversation repair: dropping leading orphan(s)", "messages", messages)
	for len(messages) > 0 && !isUserTurnStart(messages[0]) {
		messages = messages[1:]
	}
	return messages, true
}
