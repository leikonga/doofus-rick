package agent

import (
	"slices"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/llm"
)

const (
	sessionTTL     = 30 * time.Minute
	maxSessionMsgs = 30
)

type channelSession struct {
	messages   []llm.Message
	lastActive time.Time
}

func (a *Agent) getSession(id snowflake.ID) []llm.Message {
	v, ok := a.sessions.Load(id)
	if !ok {
		return nil
	}
	s := v.(channelSession)
	if time.Since(s.lastActive) > sessionTTL {
		a.sessions.Delete(id)
		return nil
	}
	return s.messages
}

func (a *Agent) putSession(id snowflake.ID, messages []llm.Message) {
	trimmed := messages
	if len(trimmed) > maxSessionMsgs {
		trimmed = trimmed[len(trimmed)-maxSessionMsgs:]
	}
	for len(trimmed) > 0 && !isUserTurnStart(trimmed[0]) {
		trimmed = trimmed[1:]
	}
	a.sessions.Store(id, channelSession{messages: slices.Clone(trimmed), lastActive: time.Now()})
}

// isUserTurnStart reports whether m begins a genuine user turn; a trimmed
// session must never start on a tool message, which the API would reject.
func isUserTurnStart(m llm.Message) bool {
	return m.Role == llm.RoleUser
}
