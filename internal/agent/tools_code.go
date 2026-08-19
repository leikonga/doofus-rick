package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/leikonga/doofus-rick/internal/llm"
)

var errRepoNotCloned = fmt.Errorf("repo not found, clone it into RICK_REPO_DIR via sys_shell first")

var errRepoBusy = fmt.Errorf("another edit is in progress on the repo, retry shortly")

type codeReadIn struct {
	Path   string `json:"path" jsonschema:"required,description=Path to the file, relative to the repo root."`
	Offset int    `json:"offset" jsonschema:"description=0-based line number to start from. Omit to start at the beginning."`
	Limit  int    `json:"limit" jsonschema:"description=Maximum number of lines to return. Omit for no limit."`
}

func (a *Agent) codeReadTool() llm.Tool {
	return llm.NewTool("code_read", "Read a file from Rick's own source checkout, cat -n style with line numbers.",
		func(_ context.Context, in codeReadIn) (llm.Result, error) {
			if a.codeedit == nil {
				return llm.Result{}, errRepoNotCloned
			}
			if !a.repoMu.TryRLock() {
				return llm.Result{}, errRepoBusy
			}
			defer a.repoMu.RUnlock()

			content, err := a.codeedit.Read(in.Path, in.Offset, in.Limit)
			if err != nil {
				return llm.Result{}, err
			}
			return llm.Result{Content: content}, nil
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

func (a *Agent) codeEditTool() llm.Tool {
	return llm.NewTool("code_edit", "Edit a file in Rick's own source checkout. command=write overwrites the whole file, command=str_replace replaces one exact match, command=insert adds a new line.",
		func(_ context.Context, in codeEditIn) (llm.Result, error) {
			if a.codeedit == nil {
				return llm.Result{}, errRepoNotCloned
			}
			if !a.repoMu.TryLock() {
				return llm.Result{}, errRepoBusy
			}
			defer a.repoMu.Unlock()

			switch in.Command {
			case "write":
				if err := a.codeedit.Write(in.Path, in.FileText); err != nil {
					return llm.Result{}, err
				}
				return llm.Result{Content: "file written"}, nil
			case "str_replace":
				n, err := a.codeedit.Replace(in.Path, in.OldStr, in.NewStr, false)
				if err != nil {
					return llm.Result{}, err
				}
				return llm.Result{Content: fmt.Sprintf("%d replacement made", n)}, nil
			case "insert":
				if err := a.codeedit.Insert(in.Path, in.InsertLine, in.NewStr); err != nil {
					return llm.Result{}, err
				}
				return llm.Result{Content: "line inserted"}, nil
			default:
				return llm.Result{}, fmt.Errorf("unknown command %q, must be write, str_replace, or insert", in.Command)
			}
		})
}

type codeShipIn struct {
	Message string `json:"message" jsonschema:"required,description=Commit message describing the change."`
}

func (a *Agent) codeShipTool() llm.Tool {
	return llm.NewTool("code_ship", "Verify Rick's own source changes (build, vet, test, boot check, migration verification if needed), then commit and push to main. Rebuild and redeploy take several minutes after this returns.",
		func(ctx context.Context, in codeShipIn) (llm.Result, error) {
			if a.codeedit == nil || a.selfcode == nil {
				return llm.Result{}, errRepoNotCloned
			}
			if in.Message == "" {
				return llm.Result{}, fmt.Errorf("commit message is required")
			}
			if !a.repoMu.TryLock() {
				return llm.Result{}, errRepoBusy
			}
			defer a.repoMu.Unlock()

			if out, err := a.runGo(ctx, "build", "./..."); err != nil {
				return llm.Result{}, fmt.Errorf("go build failed: %v\n%s", err, out)
			}
			if out, err := a.runGo(ctx, "vet", "./..."); err != nil {
				return llm.Result{}, fmt.Errorf("go vet failed: %v\n%s", err, out)
			}
			if out, err := a.runGo(ctx, "test", "./..."); err != nil {
				return llm.Result{}, fmt.Errorf("go test failed: %v\n%s", err, out)
			}

			snapshot, err := a.selfcode.Snapshot(ctx)
			if err != nil {
				return llm.Result{}, fmt.Errorf("snapshot failed: %w", err)
			}

			bin, cleanup, err := a.buildCheckBinary(ctx)
			if err != nil {
				return llm.Result{}, err
			}
			defer cleanup()
			if out, err := a.cmdRunner.Run(ctx, bin, []string{"check"}, nil); err != nil {
				return llm.Result{}, fmt.Errorf("check subcommand failed: %v\n%s", err, out)
			}
			changed, err := a.selfcode.MigrationsChanged(ctx)
			if err != nil {
				return llm.Result{}, fmt.Errorf("checking for migration changes failed: %w", err)
			}
			if changed {
				if err := a.selfcode.VerifyMigrations(ctx, snapshot); err != nil {
					return llm.Result{}, fmt.Errorf("migration verification failed: %w", err)
				}
			}

			if out, err := a.runGit(ctx, "add", "-A"); err != nil {
				return llm.Result{}, fmt.Errorf("git add failed: %v\n%s", err, out)
			}
			commitArgs := []string{
				"-C", a.config.RickRepoDir,
				"-c", "user.name=" + a.config.GitAuthorName,
				"-c", "user.email=" + a.config.GitAuthorEmail,
				"commit", "-m", in.Message,
			}
			if out, err := a.cmdRunner.Run(ctx, "git", commitArgs, a.gitEnv()); err != nil {
				return llm.Result{}, fmt.Errorf("git commit failed: %v\n%s", err, out)
			}

			if out, err := a.gitPush(ctx); err != nil {
				return llm.Result{}, fmt.Errorf("git push failed: %v\n%s", err, out)
			}

			return llm.Result{Content: "built, vetted, tested, boot-checked, committed and pushed to main. rebuild and redeploy take several minutes."}, nil
		})
}

func (a *Agent) runGo(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"-C", a.config.RickRepoDir}, args...)
	return a.cmdRunner.Run(ctx, "go", full, a.goEnv())
}

func (a *Agent) runGit(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"-C", a.config.RickRepoDir}, args...)
	return a.cmdRunner.Run(ctx, "git", full, a.gitEnv())
}

func (a *Agent) homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return a.config.WorkDir
}

func (a *Agent) goEnv() []string {
	home := a.homeDir()
	return []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"GOCACHE=" + filepath.Join(home, ".cache", "go-build"),
		"GOMODCACHE=" + filepath.Join(home, "go", "pkg", "mod"),
		"GOTOOLCHAIN=local",
		"CGO_ENABLED=0",
	}
}

func (a *Agent) gitEnv() []string {
	return []string{
		"HOME=" + a.homeDir(),
		"PATH=" + os.Getenv("PATH"),
	}
}

func (a *Agent) buildCheckBinary(ctx context.Context) (string, func(), error) {
	dir, err := os.MkdirTemp("", "rick-check-")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir for check binary: %w", err)
	}
	cleanup := func() {
		if err := os.RemoveAll(dir); err != nil {
			slog.Warn("failed to remove check binary temp dir", "dir", dir, "error", err)
		}
	}

	bin := filepath.Join(dir, "doofus-rick-check")
	args := []string{"-C", a.config.RickRepoDir, "build", "-o", bin, "./cmd/doofus-rick"}
	if out, err := a.cmdRunner.Run(ctx, "go", args, a.goEnv()); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("build check binary failed: %v\n%s", err, out)
	}
	return bin, cleanup, nil
}

// gitPush is the only place GITHUB_TOKEN is read; it reaches git only via GIT_ASKPASS, never argv.
func (a *Agent) gitPush(ctx context.Context) (string, error) {
	askpass, cleanup, err := writeAskpassHelper()
	if err != nil {
		return "", fmt.Errorf("prepare askpass helper: %w", err)
	}
	defer cleanup()

	env := []string{
		"HOME=" + a.homeDir(),
		"PATH=" + os.Getenv("PATH"),
		"GIT_ASKPASS=" + askpass,
		"GIT_TERMINAL_PROMPT=0",
		"RICK_PUSH_TOKEN=" + a.config.GitHubToken,
	}
	args := []string{"-C", a.config.RickRepoDir, "push", "origin", "HEAD:main"}
	return a.cmdRunner.Run(ctx, "git", args, env)
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
