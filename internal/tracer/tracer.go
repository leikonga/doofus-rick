package tracer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

const maxEntries = 50

type Entry struct {
	ID           string          `json:"id"`
	At           time.Time       `json:"at"`
	ChannelID    string          `json:"channel_id"`
	UserID       string          `json:"user_id"`
	SystemPrompt string          `json:"system_prompt"`
	Prompt       string          `json:"prompt"`
	Messages     json.RawMessage `json:"messages,omitempty"`
	Tools        []ToolEvent     `json:"tools,omitempty"`
	Response     string          `json:"response,omitempty"`
	Decline      bool            `json:"decline,omitempty"`
	Err          string          `json:"error,omitempty"`
	InputTokens  int64           `json:"input_tokens"`
	OutputTokens int64           `json:"output_tokens"`
	LatencyMS    int64           `json:"latency_ms"`
	Failed       bool            `json:"failed"`
}

type ToolEvent struct {
	Name   string `json:"name"`
	Input  string `json:"input"`
	Result string `json:"result"`
	IsErr  bool   `json:"is_err,omitempty"`
}

type Tracer struct {
	mu      sync.Mutex
	entries [maxEntries]*Entry
	pos     int
	count   int
	persist func(*Entry)
}

type Recording struct {
	tracer *Tracer
	entry  Entry
	start  time.Time
}

func New(persist func(*Entry)) *Tracer {
	return &Tracer{persist: persist}
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (t *Tracer) Start(channelID, userID, systemPrompt, prompt string) *Recording {
	return &Recording{
		tracer: t,
		start:  time.Now(),
		entry: Entry{
			ID:           newID(),
			At:           time.Now(),
			ChannelID:    channelID,
			UserID:       userID,
			SystemPrompt: systemPrompt,
			Prompt:       prompt,
		},
	}
}

func (r *Recording) SetMessages(data json.RawMessage) {
	r.entry.Messages = data
}

func (r *Recording) AddTool(name, input, result string, isErr bool) {
	r.entry.Tools = append(r.entry.Tools, ToolEvent{
		Name:   name,
		Input:  input,
		Result: result,
		IsErr:  isErr,
	})
}

func (r *Recording) AddTokens(input, output int64) {
	r.entry.InputTokens += input
	r.entry.OutputTokens += output
}

// Finish seals the recording and routes it: successes go to the in-memory
// ring, failures (err or decline) call the persist hook.
func (r *Recording) Finish(response string, decline bool, err error) *Entry {
	r.entry.Response = response
	r.entry.Decline = decline
	if err != nil {
		r.entry.Err = err.Error()
	}
	r.entry.LatencyMS = time.Since(r.start).Milliseconds()
	r.entry.Failed = err != nil || decline
	e := r.entry
	ep := &e
	if r.entry.Failed {
		if r.tracer.persist != nil {
			r.tracer.persist(ep)
		}
	} else {
		r.tracer.addSuccess(ep)
	}
	return ep
}

func (t *Tracer) addSuccess(e *Entry) {
	t.mu.Lock()
	t.entries[t.pos%maxEntries] = e
	t.pos++
	if t.count < maxEntries {
		t.count++
	}
	t.mu.Unlock()
}

// RecentSuccesses returns in-memory success entries in chronological order.
func (t *Tracer) RecentSuccesses() []*Entry {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.count == 0 {
		return nil
	}
	out := make([]*Entry, t.count)
	start := t.pos - t.count
	for i := 0; i < t.count; i++ {
		out[i] = t.entries[(start+i)%maxEntries]
	}
	return out
}

// FindByID looks up an in-memory entry by ID.
func (t *Tracer) FindByID(id string) *Entry {
	t.mu.Lock()
	defer t.mu.Unlock()
	start := t.pos - t.count
	for i := 0; i < t.count; i++ {
		e := t.entries[(start+i)%maxEntries]
		if e != nil && e.ID == id {
			return e
		}
	}
	return nil
}
