package agent

import (
	"slices"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/disgoorg/snowflake/v2"
)

const (
	sessionTTL     = 30 * time.Minute
	maxSessionMsgs = 30
)

type channelSession struct {
	messages   []anthropic.MessageParam
	lastActive time.Time
}

func (a *Agent) getSession(id snowflake.ID) []anthropic.MessageParam {
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

func (a *Agent) putSession(id snowflake.ID, messages []anthropic.MessageParam) {
	trimmed := messages
	if len(trimmed) > maxSessionMsgs {
		trimmed = trimmed[len(trimmed)-maxSessionMsgs:]
	}
	for len(trimmed) > 0 && !isUserTurnStart(trimmed[0]) {
		trimmed = trimmed[1:]
	}
	a.sessions.Store(id, channelSession{messages: slices.Clone(trimmed), lastActive: time.Now()})
}

// isUserTurnStart reports whether m begins a genuine user turn, i.e., a user
// message whose first content block is not a tool_result. Trimming a session
// must not leave a leading tool_result, which would have no preceding tool_use
// and be rejected by the API.
func isUserTurnStart(m anthropic.MessageParam) bool {
	if m.Role != anthropic.MessageParamRoleUser {
		return false
	}
	return len(m.Content) == 0 || m.Content[0].OfToolResult == nil
}
