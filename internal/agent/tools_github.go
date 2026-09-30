package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/leikonga/doofus-rick/internal/llm"
)

const githubRepo = "leikonga/doofus-rick"

type githubIssueIn struct {
	Action  string `json:"action" jsonschema:"required,enum=list,enum=comment,enum=close,description=list shows open issues; comment adds a comment; close closes an issue (optionally with a comment)."`
	Number  int    `json:"number" jsonschema:"description=Issue number. Required for comment and close."`
	Comment string `json:"comment" jsonschema:"description=Comment text. Required for comment; optional for close."`
}

func (a *Agent) githubIssueTool() llm.Tool {
	return llm.NewTool("github_issue",
		"List, comment on or close issues of Rick's own GitHub repo ("+githubRepo+"). Uses the push token internally.",
		func(ctx context.Context, in githubIssueIn) (llm.Result, error) {
			if a.config.GitHubToken == "" {
				return llm.Result{}, fmt.Errorf("no GITHUB_TOKEN configured")
			}
			base := "https://api.github.com/repos/" + githubRepo + "/issues"
			switch in.Action {
			case "list":
				out, err := a.githubDo(ctx, http.MethodGet, base+"?state=open&per_page=50", nil)
				if err != nil {
					return llm.Result{}, err
				}
				var issues []struct {
					Number int    `json:"number"`
					Title  string `json:"title"`
					PR     any    `json:"pull_request"`
				}
				if err := json.Unmarshal(out, &issues); err != nil {
					return llm.Result{}, fmt.Errorf("parse issues: %w", err)
				}
				s := ""
				for _, i := range issues {
					if i.PR == nil {
						s += fmt.Sprintf("#%d %s\n", i.Number, i.Title)
					}
				}
				if s == "" {
					s = "no open issues"
				}
				return llm.Result{Content: s}, nil
			case "comment", "close":
				if in.Number <= 0 {
					return llm.Result{}, fmt.Errorf("number is required")
				}
				if in.Action == "comment" && in.Comment == "" {
					return llm.Result{}, fmt.Errorf("comment is required")
				}
				url := fmt.Sprintf("%s/%d", base, in.Number)
				if in.Comment != "" {
					if _, err := a.githubDo(ctx, http.MethodPost, url+"/comments", map[string]any{"body": in.Comment}); err != nil {
						return llm.Result{}, err
					}
				}
				if in.Action == "close" {
					if _, err := a.githubDo(ctx, http.MethodPatch, url, map[string]any{"state": "closed", "state_reason": "completed"}); err != nil {
						return llm.Result{}, err
					}
				}
				return llm.Result{Content: fmt.Sprintf("ok: %s #%d", in.Action, in.Number)}, nil
			}
			return llm.Result{}, fmt.Errorf("unknown action %q", in.Action)
		})
}

func (a *Agent) githubDo(ctx context.Context, method, url string, body any) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.config.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read github response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github %s: %d %s", method, resp.StatusCode, string(out[:min(len(out), 300)]))
	}
	return out, nil
}
