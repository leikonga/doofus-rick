// Package llm is a vendor-neutral chat completion abstraction. Only client.go
// may import the OpenRouter SDK.
package llm

import (
	"context"
	"encoding/json"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ContentPart struct {
	Type     string // "text" | "image_url" | "file"
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
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

func TextPart(text string) ContentPart {
	return ContentPart{Type: "text", Text: text}
}

func ImagePart(url string) ContentPart {
	return ContentPart{Type: "image_url", ImageURL: url}
}

func FileContentPart(filename, data string) ContentPart {
	return ContentPart{Type: "file", File: &FilePart{Filename: filename, Data: data}}
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
	Content  string
	Done     bool          // terminal, decline without text
	Response *RickResponse // terminal, tool produced the reply itself
}

// Tool is a vendor-neutral tool definition. Schema is derived from a Go
// struct by NewTool; definitions built this way carry no vendor types.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Execute     func(ctx context.Context, input json.RawMessage) (Result, error)
}

type Tools []Tool

func (ts Tools) Find(name string) (Tool, bool) {
	for _, t := range ts {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}
