package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/leikonga/doofus-rick/internal/codeedit"
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/runtimehome"
	"github.com/leikonga/doofus-rick/internal/selfcode"
)

type codeTools struct {
	editor   *codeedit.Editor
	selfcode *selfcode.Selfcode
	runner   selfcode.Runner
	repoMu   sync.RWMutex
	config   *config.Config
	deploys  *runtimehome.Journal
}

var errRepoNotCloned = fmt.Errorf("repo not found, clone it into RICK_REPO_DIR via sys_shell first")

var errRepoBusy = fmt.Errorf("another edit is in progress on the repo, retry shortly")

type codeReadIn struct {
	Path   string `json:"path" jsonschema:"required,description=Path to the file, relative to the repo root."`
	Offset int    `json:"offset" jsonschema:"description=0-based line number to start from. Omit to start at the beginning."`
	Limit  int    `json:"limit" jsonschema:"description=Maximum number of lines to return. Omit for no limit."`
}

func (c *codeTools) codeReadTool() llm.Tool {
	return llm.NewTool("code_read", "Read a file from Rick's own source checkout, cat -n style with line numbers.",
		func(_ context.Context, in codeReadIn) (llm.Result, error) {
			if c.editor == nil {
				return llm.Result{}, errRepoNotCloned
			}
			if !c.repoMu.TryRLock() {
				return llm.Result{}, errRepoBusy
			}
			defer c.repoMu.RUnlock()

			content, err := c.editor.Read(in.Path, in.Offset, in.Limit)
			if err != nil {
				return llm.Result{}, err
			}
			return llm.Continue(content), nil
		})
}

type codeEditIn struct {
	Command    string `json:"command" jsonschema:"required,enum=write,enum=str_replace,enum=insert,description=Which edit operation to perform."`
	Path       string `json:"path" jsonschema:"required,description=Path to the file, relative to the repo root."`
	FileText   string `json:"file_text" jsonschema:"description=Full file content. Required for command=write."`
	OldStr     string `json:"old_str" jsonschema:"description=Exact text to replace. Required for command=str_replace."`
	NewStr     string `json:"new_str" jsonschema:"description=Replacement text for command=str_replace, or the line to insert for command=insert."`
	InsertLine int    `json:"insert_line" jsonschema:"description=Line number after which to insert, 0 for the beginning of the file. Required for command=insert."`
}

func (c *codeTools) codeEditTool() llm.Tool {
	return llm.NewTool("code_edit", "Edit a file in Rick's own source checkout. command=write overwrites the whole file, command=str_replace replaces one exact match, command=insert adds a new line.",
		func(_ context.Context, in codeEditIn) (llm.Result, error) {
			if c.editor == nil {
				return llm.Result{}, errRepoNotCloned
			}
			if !c.repoMu.TryLock() {
				return llm.Result{}, errRepoBusy
			}
			defer c.repoMu.Unlock()

			switch in.Command {
			case "write":
				if err := c.editor.Write(in.Path, in.FileText); err != nil {
					return llm.Result{}, err
				}
				return llm.Continue("file written"), nil
			case "str_replace":
				n, err := c.editor.Replace(in.Path, in.OldStr, in.NewStr, false)
				if err != nil {
					return llm.Result{}, err
				}
				return llm.Continue(fmt.Sprintf("%d replacement made", n)), nil
			case "insert":
				if err := c.editor.Insert(in.Path, in.InsertLine, in.NewStr); err != nil {
					return llm.Result{}, err
				}
				return llm.Continue("line inserted"), nil
			default:
				return llm.Result{}, fmt.Errorf("unknown command %q, must be write, str_replace, or insert", in.Command)
			}
		})
}

type codeShipIn struct {
	Message string `json:"message" jsonschema:"required,description=Commit message describing the change."`
}

func (c *codeTools) codeShipTool(origin turnOrigin) llm.Tool {
	return llm.NewTool("code_ship", "Verify Rick's own source changes (build, vet, test, migration verification if needed), then commit and push to main. Rebuild and redeploy take several minutes after this returns.",
		func(ctx context.Context, in codeShipIn) (llm.Result, error) {
			if c.editor == nil || c.selfcode == nil {
				return llm.Result{}, errRepoNotCloned
			}
			if in.Message == "" {
				return llm.Result{}, fmt.Errorf("commit message is required")
			}
			if !c.repoMu.TryLock() {
				return llm.Result{}, errRepoBusy
			}
			defer c.repoMu.Unlock()

			if out, err := c.runGo(ctx, "build", "./..."); err != nil {
				return llm.Result{}, fmt.Errorf("go build failed: %w\n%s", err, out)
			}
			if out, err := c.runGo(ctx, "vet", "./..."); err != nil {
				return llm.Result{}, fmt.Errorf("go vet failed: %w\n%s", err, out)
			}
			if out, err := c.runGo(ctx, "test", "./..."); err != nil {
				return llm.Result{}, fmt.Errorf("go test failed: %w\n%s", err, out)
			}

			prompt, err := os.ReadFile(c.config.SystemPromptFile)
			if err != nil {
				return llm.Result{}, fmt.Errorf("system prompt file: %w", err)
			}
			if strings.TrimSpace(string(prompt)) == "" {
				return llm.Result{}, fmt.Errorf("system prompt file %q is empty", c.config.SystemPromptFile)
			}

			snapshot, err := c.selfcode.Snapshot(ctx)
			if err != nil {
				return llm.Result{}, fmt.Errorf("snapshot failed: %w", err)
			}

			changed, err := c.selfcode.MigrationsChanged(ctx)
			if err != nil {
				return llm.Result{}, fmt.Errorf("checking for migration changes failed: %w", err)
			}
			if changed {
				if err := c.selfcode.VerifyMigrations(ctx, snapshot); err != nil {
					return llm.Result{}, fmt.Errorf("migration verification failed: %w", err)
				}
			}

			if out, err := c.runGit(ctx, "add", "-A"); err != nil {
				return llm.Result{}, fmt.Errorf("git add failed: %w\n%s", err, out)
			}
			commitArgs := []string{
				"-C", c.config.RickRepoDir,
				"-c", "user.name=" + c.config.GitAuthorName,
				"-c", "user.email=" + c.config.GitAuthorEmail,
				"commit", "-m", in.Message,
			}
			if out, err := c.runner.Run(ctx, "git", commitArgs, c.gitEnv()); err != nil {
				return llm.Result{}, fmt.Errorf("git commit failed: %w\n%s", err, out)
			}

			if out, err := c.gitPush(ctx); err != nil {
				return llm.Result{}, fmt.Errorf("git push failed: %w\n%s", err, out)
			}
			c.recordShip(ctx, origin, in.Message)

			return llm.Continue("built, vetted, tested, boot-checked, committed and pushed to main. rebuild and redeploy take several minutes."), nil
		})
}

func (c *codeTools) recordShip(ctx context.Context, origin turnOrigin, message string) {
	if c.deploys == nil {
		slog.Debug("deploy journal unavailable, not recording ship")
		return
	}
	out, err := c.runGit(ctx, "rev-parse", "HEAD")
	commit := strings.TrimSpace(out)
	if err != nil || commit == "" {
		slog.Warn("failed to read pushed commit, not recording ship", "output", out, "error", err)
		return
	}
	err = c.deploys.Append(runtimehome.DeployRecord{
		Kind:      runtimehome.DeployShip,
		Commit:    commit,
		ChannelID: origin.ChannelID.String(),
		Requester: origin.AuthorID.String(),
		Summary:   message,
		At:        time.Now(),
	})
	if err != nil {
		slog.Warn("failed to record ship in deploy journal", "commit", commit, "error", err)
	}
}

func (c *codeTools) runGo(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"-C", c.config.RickRepoDir}, args...)
	return c.runner.Run(ctx, "go", full, c.goEnv())
}

func (c *codeTools) runGit(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"-C", c.config.RickRepoDir}, args...)
	return c.runner.Run(ctx, "git", full, c.gitEnv())
}

func (c *codeTools) homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return c.config.WorkDir
}

func (c *codeTools) goEnv() []string {
	home := c.homeDir()
	return []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"GOCACHE=" + filepath.Join(home, ".cache", "go-build"),
		"GOMODCACHE=" + filepath.Join(home, "go", "pkg", "mod"),
		"GOTOOLCHAIN=local",
		"CGO_ENABLED=0",
	}
}

func (c *codeTools) gitEnv() []string {
	return []string{
		"HOME=" + c.homeDir(),
		"PATH=" + os.Getenv("PATH"),
	}
}

// gitPush is the only place GITHUB_TOKEN is read; it reaches git only via GIT_ASKPASS, never argv.
func (c *codeTools) gitPush(ctx context.Context) (string, error) {
	askpass, cleanup, err := writeAskpassHelper()
	if err != nil {
		return "", fmt.Errorf("prepare askpass helper: %w", err)
	}
	defer cleanup()

	env := []string{
		"HOME=" + c.homeDir(),
		"PATH=" + os.Getenv("PATH"),
		"GIT_ASKPASS=" + askpass,
		"GIT_TERMINAL_PROMPT=0",
		"RICK_PUSH_TOKEN=" + c.config.GitHubToken,
	}
	args := []string{"-C", c.config.RickRepoDir, "push", "origin", "HEAD:main"}
	return c.runner.Run(ctx, "git", args, env)
}

// GitHub accepts the token as either the username or the password prompt, so echoing it for both is sufficient.
func writeAskpassHelper() (string, func(), error) {
	f, err := os.CreateTemp("", "rick-askpass-*")
	if err != nil {
		return "", nil, fmt.Errorf("create askpass helper: %w", err)
	}
	path := f.Name()
	cleanup := func() {
		if err := os.Remove(path); err != nil {
			slog.Warn("failed to remove askpass helper", "path", path, "error", err)
		}
	}

	if _, err := f.WriteString("#!/bin/sh\necho \"$RICK_PUSH_TOKEN\"\n"); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("write askpass helper: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("close askpass helper: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("chmod askpass helper: %w", err)
	}
	return path, cleanup, nil
}
