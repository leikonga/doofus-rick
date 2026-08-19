package agent

import (
	"context"

	"github.com/leikonga/doofus-rick/internal/client"
	"github.com/leikonga/doofus-rick/internal/llm"
)

type shellExecIn struct {
	Command string `json:"command" jsonschema:"required,description=Shell command to run."`
}

func (a *Agent) shellExecTool() llm.Tool {
	return llm.NewTool("sys_shell",
		"Run a shell command and return stdout+stderr. "+
			"Runs as an unprivileged user in an Alpine Linux environment. "+
			"Available: bash, curl, jq, git, openssh-client, python3, uv, make, coreutils, sqlite3, diffutils, patch, bc, file, dig, openssl, imagemagick. "+
			"Working directory is /rick/work; persistent across calls, use it freely to store files, scripts, databases, cloned repos, etc. "+
			"HOME is also /rick/work. "+
			"Python packages can be installed inline with: uv run --with <pkg> python3 -c '...'.",
		func(ctx context.Context, in shellExecIn) (llm.Result, error) {
			return llm.Result{Content: a.shell.Exec(ctx, in.Command, client.DefaultOutputLimit)}, nil
		})
}

type checkLogsIn struct{}

func (a *Agent) checkLogsTool() llm.Tool {
	return llm.NewTool("sys_logs", "Check recent warnings and errors from Rick's own process logs. Use when asked why Rick didn't respond or what went wrong.",
		func(_ context.Context, _ checkLogsIn) (llm.Result, error) {
			return llm.Result{Content: a.logBuf.Recent()}, nil
		})
}
