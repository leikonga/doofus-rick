package agent

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/runtimehome"
	"github.com/leikonga/doofus-rick/internal/sandbox"
	"github.com/leikonga/doofus-rick/internal/selbst"
	"github.com/leikonga/doofus-rick/internal/shell"
)

type shellExecIn struct {
	Command string `json:"command" jsonschema:"required,description=Shell command to run."`
}

func (a *Agent) shellExecTool() llm.Tool {
	return llm.NewTool("sys_shell", a.shellDesc,
		func(ctx context.Context, in shellExecIn) (llm.Result, error) {
			return llm.Result{Content: a.shell.Exec(ctx, in.Command, shell.DefaultOutputLimit)}, nil
		})
}

func shellDescription(shellUser, workDir, pprofAddr string, tools []sandbox.Tool) string {
	var sb strings.Builder
	sb.WriteString("Run a shell command with bash and return stdout+stderr. ")
	sb.WriteString("Separate sys_shell calls in one response run at the same time in no guaranteed order: use them for independent tasks. ")
	sb.WriteString("Steps that depend on each other belong in ONE call as a sequence, chained with && or written as a heredoc script. ")
	fmt.Fprintf(&sb, "Alpine Linux; runs as the unprivileged user %s, not as the bot process. ", shellUser)
	fmt.Fprintf(&sb, "Working directory and HOME are %s, persistent across calls and redeploys; store files, scripts, databases and cloned repos there. ", workDir)
	sb.WriteString("Python packages: uv run --with <pkg> python3 -c '...', or uvx <tool>. ")
	sb.WriteString("The Go toolchain is installed; go install puts Go tools on PATH. ")
	if pprofAddr != "" {
		fmt.Fprintf(&sb, "Profile the running bot via pprof, e.g. go tool pprof -top http://%s/debug/pprof/heap. ", selbst.LoopbackAddr(pprofAddr))
	}
	sb.WriteString("\nInstalled tools:\n")
	for _, t := range tools {
		name := t.Package
		if len(t.Commands) > 0 {
			name = strings.Join(t.Commands, ", ")
		}
		fmt.Fprintf(&sb, "- %s: %s\n", name, t.Purpose)
	}
	sb.WriteString("To add a native Alpine package, add a line to internal/sandbox/tools.txt in your source and ship it.")
	return sb.String()
}

type runtimeLogs interface {
	Problems(since time.Time) ([]runtimehome.LogEntry, error)
	CrashReports() ([]runtimehome.CrashReport, error)
}

const (
	defaultLogHours = 24
	maxLogHours     = 14 * 24
	logTimeLayout   = "2006-01-02 15:04:05"
)

type checkLogsIn struct {
	Hours int `json:"hours" jsonschema:"description=How many hours back to look. Defaults to 24; clamped to 1..336."`
}

func (a *Agent) checkLogsTool() llm.Tool {
	return llm.NewTool("sys_logs",
		"Read warnings and errors from Rick's persistent process logs (they survive restarts) plus any crash reports from earlier boots. "+
			"Use when asked why Rick didn't respond or what went wrong.",
		func(_ context.Context, in checkLogsIn) (llm.Result, error) {
			return llm.Result{Content: a.logReport(in.Hours, time.Now())}, nil
		})
}

func clampLogHours(hours int) int {
	switch {
	case hours == 0:
		return defaultLogHours
	case hours < 1:
		return 1
	case hours > maxLogHours:
		return maxLogHours
	default:
		return hours
	}
}

func (a *Agent) logReport(hours int, now time.Time) string {
	if a.runtimeLogs == nil {
		return "log directory unavailable: runtime home failed to open, so no persistent logs or crash reports exist"
	}
	hours = clampLogHours(hours)
	entries, logErr := a.runtimeLogs.Problems(now.Add(-time.Duration(hours) * time.Hour))
	crashes, crashErr := a.runtimeLogs.CrashReports()

	var tail strings.Builder
	if len(crashes) > 0 {
		tail.WriteString("\ncrash reports from earlier boots:\n")
		for _, c := range crashes {
			fmt.Fprintf(&tail, "%s (%d bytes, %s)\n", c.Path, c.Size, c.ModTime.UTC().Format(logTimeLayout))
		}
	}
	for _, err := range []error{logErr, crashErr} {
		if err != nil {
			fmt.Fprintf(&tail, "\nread error: %v\n", err)
		}
	}

	header := fmt.Sprintf("warnings and errors in the last %dh (UTC):\n", hours)
	if len(entries) == 0 {
		header = fmt.Sprintf("no warnings or errors in the last %dh\n", hours)
	}
	const omittedNote = "(older entries omitted)\n"
	budget := shell.DefaultOutputLimit - len(header) - len(omittedNote) - tail.Len()
	lines := make([]string, 0, len(entries))
	for _, entrie := range slices.Backward(entries) {
		line := formatLogEntry(entrie)
		if budget-len(line)-1 < 0 {
			header += omittedNote
			break
		}
		budget -= len(line) + 1
		lines = append(lines, line)
	}
	slices.Reverse(lines)

	var out strings.Builder
	out.WriteString(header)
	for _, line := range lines {
		out.WriteString(line)
		out.WriteByte('\n')
	}
	out.WriteString(tail.String())
	return strings.TrimRight(out.String(), "\n")
}

func formatLogEntry(e runtimehome.LogEntry) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s %s", e.Time.UTC().Format(logTimeLayout), e.Level, e.Msg)
	for _, attr := range e.Attrs {
		value := attr.Value
		if value == "" || strings.ContainsAny(value, " \t\n\"=") {
			value = strconv.Quote(value)
		}
		fmt.Fprintf(&sb, " %s=%s", attr.Key, value)
	}
	return sb.String()
}
