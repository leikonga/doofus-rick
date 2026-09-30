package llm

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/OpenRouterTeam/go-sdk/models/operations"
)

type RerankRequest struct {
	Model     string
	Query     string
	Documents []string
	TopN      int
}

type RerankResult struct {
	Index int
	Score float64
}

type RerankResponse struct {
	Results     []RerankResult
	TotalTokens int64
}

func (c *Client) Rerank(ctx context.Context, req RerankRequest) (RerankResponse, error) {
	docs := make([]operations.Document, len(req.Documents))
	for i, d := range req.Documents {
		docs[i] = operations.CreateDocumentStr(d)
	}
	sdkReq := operations.CreateRerankRequest{Model: req.Model, Query: req.Query, Documents: docs}
	if req.TopN > 0 {
		topN := int64(req.TopN)
		sdkReq.TopN = &topN
	}

	start := time.Now()
	res, err := c.sdk.Rerank.Rerank(ctx, sdkReq)
	if err != nil {
		slog.Warn("openrouter rerank request failed", "model", req.Model, "documents", len(req.Documents), "latency_ms", time.Since(start).Milliseconds(), "error", err)
		return RerankResponse{}, err
	}
	if res == nil || res.CreateRerankResponseBody == nil {
		return RerankResponse{}, fmt.Errorf("llm: empty rerank response")
	}

	body := res.CreateRerankResponseBody
	resp := RerankResponse{Results: make([]RerankResult, len(body.Results))}
	for i, r := range body.Results {
		resp.Results[i] = RerankResult{Index: int(r.Index), Score: r.RelevanceScore}
	}
	if body.Usage != nil && body.Usage.TotalTokens != nil {
		resp.TotalTokens = *body.Usage.TotalTokens
	}
	slog.Info("openrouter rerank completed", "model", req.Model, "documents", len(req.Documents),
		"results", len(resp.Results), "total_tokens", resp.TotalTokens, "latency_ms", time.Since(start).Milliseconds())
	return resp, nil
}
