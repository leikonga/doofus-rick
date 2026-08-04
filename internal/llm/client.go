package llm

import (
	"context"
	"fmt"
	"strings"

	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
)

type StopReason string

const (
	StopToolCalls StopReason = "tool_calls"
	StopEnd       StopReason = "stop"
	StopOther     StopReason = ""
)

type Client struct {
	sdk *openrouter.OpenRouter
}

func NewClient(apiKey string) *Client {
	return &Client{sdk: openrouter.New(openrouter.WithSecurity(apiKey))}
}

type CompletionRequest struct {
	Model     string
	MaxTokens int64
	System    string
	Messages  []Message
	Tools     []Tool
}

type CompletionResponse struct {
	Message      Message
	StopReason   StopReason
	InputTokens  int64
	OutputTokens int64
}

type EmbeddingResponse struct {
	Embeddings  [][]float32
	InputTokens int64
}

func (c *Client) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	chatMessages := make([]components.ChatMessages, 0, len(req.Messages)+1)
	chatMessages = append(chatMessages, components.CreateChatMessagesSystem(components.ChatSystemMessage{
		Content: components.CreateChatSystemMessageContentStr(req.System),
	}))
	for _, m := range req.Messages {
		chatMessages = append(chatMessages, toSDKMessage(m))
	}

	maxTokens := req.MaxTokens
	chatReq := components.ChatRequest{
		Model:     &req.Model,
		Messages:  chatMessages,
		MaxTokens: optionalnullable.From(&maxTokens),
		Tools:     toSDKTools(req.Tools),
	}

	res, err := c.sdk.Chat.Send(ctx, chatReq, nil)
	if err != nil {
		return CompletionResponse{}, err
	}
	if res.ChatResult == nil || len(res.ChatResult.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("llm: empty response from model")
	}

	choice := res.ChatResult.Choices[0]
	resp := CompletionResponse{
		Message:    fromSDKAssistantMessage(choice.Message),
		StopReason: stopReasonFrom(choice.FinishReason),
	}
	if res.ChatResult.Usage != nil {
		resp.InputTokens = res.ChatResult.Usage.PromptTokens
		resp.OutputTokens = res.ChatResult.Usage.CompletionTokens
	}
	return resp, nil
}

func (c *Client) Embed(ctx context.Context, req CompletionRequest) (EmbeddingResponse, error) {
	var resp EmbeddingResponse
	return resp, nil
}

func stopReasonFrom(fr *components.ChatFinishReasonEnum) StopReason {
	if fr == nil {
		return StopOther
	}
	switch *fr {
	case components.ChatFinishReasonEnumToolCalls:
		return StopToolCalls
	case components.ChatFinishReasonEnumStop:
		return StopEnd
	default:
		return StopOther
	}
}

func toSDKTools(tools []Tool) []components.ChatFunctionTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]components.ChatFunctionTool, len(tools))
	for i, t := range tools {
		desc := t.Description
		out[i] = components.CreateChatFunctionToolChatFunctionToolFunction(components.ChatFunctionToolFunction{
			Type: components.ChatFunctionToolTypeFunction,
			Function: components.ChatFunctionToolFunctionFunction{
				Name:        t.Name,
				Description: &desc,
				Parameters:  t.Schema,
			},
		})
	}
	return out
}

func toSDKMessage(m Message) components.ChatMessages {
	switch m.Role {
	case RoleUser:
		return components.CreateChatMessagesUser(components.ChatUserMessage{
			Content: toSDKUserContent(m.Parts),
			Name:    optionalString(m.Name),
		})
	case RoleAssistant:
		return components.CreateChatMessagesAssistant(components.ChatAssistantMessage{
			Content:   toSDKAssistantContent(m.Parts),
			ToolCalls: toSDKToolCalls(m.ToolCalls),
		})
	case RoleTool:
		return components.CreateChatMessagesTool(components.ChatToolMessage{
			Content:    components.CreateChatToolMessageContentStr(partsText(m.Parts)),
			ToolCallID: m.ToolCallID,
		})
	default:
		return components.CreateChatMessagesUser(components.ChatUserMessage{
			Content: toSDKUserContent(m.Parts),
		})
	}
}

func toSDKToolCalls(calls []ToolCall) []components.ChatToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]components.ChatToolCall, len(calls))
	for i, c := range calls {
		out[i] = components.ChatToolCall{
			ID:   c.ID,
			Type: components.ChatToolCallTypeFunction,
			Function: components.ChatToolCallFunction{
				Name:      c.Name,
				Arguments: c.Arguments,
			},
		}
	}
	return out
}

func toSDKUserContent(parts []ContentPart) components.ChatUserMessageContent {
	if len(parts) == 1 && parts[0].Type == "text" {
		return components.CreateChatUserMessageContentStr(parts[0].Text)
	}
	items := make([]components.ChatContentItems, 0, len(parts))
	for _, p := range parts {
		if item, ok := toSDKContentItem(p); ok {
			items = append(items, item)
		}
	}
	return components.CreateChatUserMessageContentArrayOfChatContentItems(items)
}

func toSDKAssistantContent(parts []ContentPart) optionalnullable.OptionalNullable[components.ChatAssistantMessageContent] {
	text := partsText(parts)
	if text == "" {
		return nil
	}
	content := components.CreateChatAssistantMessageContentStr(text)
	return optionalnullable.From(&content)
}

func toSDKContentItem(p ContentPart) (components.ChatContentItems, bool) {
	switch p.Type {
	case "text":
		return components.CreateChatContentItemsText(components.ChatContentText{Text: p.Text}), true
	case "image_url":
		return components.CreateChatContentItemsImageURL(components.ChatContentImage{
			ImageURL: components.ChatContentImageImageURL{URL: p.ImageURL},
		}), true
	case "file":
		if p.File == nil {
			return components.ChatContentItems{}, false
		}
		filename := p.File.Filename
		data := p.File.Data
		return components.CreateChatContentItemsFile(components.ChatContentFile{
			File: components.File{Filename: &filename, FileData: &data},
		}), true
	default:
		return components.ChatContentItems{}, false
	}
}

func partsText(parts []ContentPart) string {
	var sb strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func fromSDKAssistantMessage(m components.ChatAssistantMessage) Message {
	out := Message{Role: RoleAssistant}
	if m.Content.IsSet() {
		if c, ok := m.Content[true]; ok && c != nil {
			if c.Str != nil {
				out.Parts = append(out.Parts, TextPart(*c.Str))
			}
			for _, item := range c.ArrayOfChatContentItems {
				if item.ChatContentText != nil {
					out.Parts = append(out.Parts, TextPart(item.ChatContentText.Text))
				}
			}
		}
	}
	for _, tc := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out
}
