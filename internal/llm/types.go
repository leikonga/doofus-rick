package llm

import (
	"context"
	"encoding/json"
	"strings"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type PartType string

const (
	PartText     PartType = "text"
	PartImageURL PartType = "image_url"
	PartFile     PartType = "file"
)

type ContentPart struct {
	Type     PartType
	Text     string
	ImageURL string
	File     *FilePart
}

type FilePart struct {
	Filename string
	Data     string
}

type Message struct {
	Role       Role
	Name       string
	Parts      []ContentPart
	ToolCalls  []ToolCall
	ToolCallID string
	// ReasoningDetails is the provider's opaque reasoning payload on an
	// assistant message, sent back unchanged so reasoning survives tool calls.
	ReasoningDetails json.RawMessage
}

func (m Message) Text() string {
	var sb strings.Builder
	for _, p := range m.Parts {
		if p.Type == PartText {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

func TextPart(text string) ContentPart {
	return ContentPart{Type: PartText, Text: text}
}

func ImagePart(url string) ContentPart {
	return ContentPart{Type: PartImageURL, ImageURL: url}
}

func FileContentPart(filename, data string) ContentPart {
	return ContentPart{Type: PartFile, File: &FilePart{Filename: filename, Data: data}}
}

func NewUserMessage(parts ...ContentPart) Message {
	return Message{Role: RoleUser, Parts: parts}
}

func NewToolResultMessage(toolCallID, content string) Message {
	return Message{Role: RoleTool, ToolCallID: toolCallID, Parts: []ContentPart{TextPart(content)}}
}

// RickResponse is the terminal outcome of a tool that decides the reply
// itself rather than returning content for the model to keep working with.
type RickResponse struct {
	Text    string
	Decline bool
	Emoji   string
}

// Result is what a tool's Execute function returns to the calling loop.
type Result struct {
	content  string
	endsTurn bool
	response *RickResponse
}

func Continue(content string) Result {
	return Result{content: content}
}

func EndTurn(content string) Result {
	return Result{content: content, endsTurn: true}
}

func Reply(resp RickResponse) Result {
	return Result{response: &resp}
}

func (r Result) Content() string {
	return r.content
}

func (r Result) EndsTurn() bool {
	return r.endsTurn
}

func (r Result) Response() *RickResponse {
	return r.response
}

type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Execute     func(ctx context.Context, input json.RawMessage) (Result, error)
}

type Tools []Tool

// Find prefers an exact match and falls back to a case-insensitive one,
// since models occasionally miscase a declared tool name.
func (ts Tools) Find(name string) (Tool, bool) {
	for _, t := range ts {
		if t.Name == name {
			return t, true
		}
	}
	for _, t := range ts {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return Tool{}, false
}

func (ts Tools) Names() []string {
	names := make([]string, len(ts))
	for i, t := range ts {
		names[i] = t.Name
	}
	return names
}
