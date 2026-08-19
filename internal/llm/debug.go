package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/OpenRouterTeam/go-sdk/models/components"
)

// DebugUpstreamBody re-sends req with debug.echo_upstream_body enabled and
// returns the raw transformed request OpenRouter forwards to the upstream
// provider. It's a manual troubleshooting tool (e.g. for confirming whether a
// model actually supports tool calling, or why routing behaves unexpectedly)
// and is not called anywhere in the normal request path. The typed SDK
// doesn't model the debug chunk, so this bypasses it and reads the raw SSE
// stream directly. Requires streaming, per OpenRouter's API.
func (c *Client) DebugUpstreamBody(ctx context.Context, req CompletionRequest) (string, error) {
	chatReq := buildChatRequest(req)
	stream := true
	chatReq.Stream = &stream
	echo := true
	chatReq.Debug = &components.ChatDebugOptions{EchoUpstreamBody: &echo}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return "", fmt.Errorf("llm: marshal debug request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://openrouter.ai/api/v1/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("llm: build debug request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("llm: debug request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// The debug chunk is the first SSE "data:" line in the stream.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if data, ok := strings.CutPrefix(line, "data:"); ok {
			return strings.TrimSpace(data), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("llm: reading debug stream: %w", err)
	}
	return "", fmt.Errorf("llm: no data received in debug stream")
}
