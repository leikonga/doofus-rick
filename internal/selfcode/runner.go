package selfcode

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// ExecRunner runs commands via os/exec. If env is nil the child process
// inherits the current process environment, matching os/exec semantics.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s: %w", name, err)
	}
	return out.String(), nil
}
