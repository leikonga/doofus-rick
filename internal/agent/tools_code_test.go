package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/disgoorg/disgo/events"
	"github.com/leikonga/doofus-rick/internal/codeedit"
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/selfcode"
)

const shipTestToken = "secret-token-xyz"

type recordedCall struct {
	name string
	args []string
	env  []string
}

type fakeCmdRunner struct {
	mu      sync.Mutex
	calls   []recordedCall
	errs    map[string]error
	results map[string]string
}

func newFakeCmdRunner() *fakeCmdRunner {
	return &fakeCmdRunner{errs: map[string]error{}, results: map[string]string{}}
}

func (f *fakeCmdRunner) Run(_ context.Context, name string, args []string, env []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, recordedCall{
		name: name,
		args: append([]string(nil), args...),
		env:  append([]string(nil), env...),
	})
	stage := stageOf(name, args)
	return f.results[stage], f.errs[stage]
}

func (f *fakeCmdRunner) called(stage string) bool {
	_, ok := f.callFor(stage)
	return ok
}

func (f *fakeCmdRunner) callFor(stage string) (recordedCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if stageOf(c.name, c.args) == stage {
			return c, true
		}
	}
	return recordedCall{}, false
}

// stageOf names a call by what it represents, so tests can target a gate stage without exact argv matching.
func stageOf(name string, args []string) string {
	switch name {
	case "go":
		switch {
		case slices.Contains(args, "-o"):
			return "go_build_check"
		case slices.Contains(args, "build"):
			return "go_build"
		case slices.Contains(args, "vet"):
			return "go_vet"
		case slices.Contains(args, "test"):
			return "go_test"
		}
	case "git":
		switch {
		case slices.Contains(args, "status"):
			return "git_status"
		case slices.Contains(args, "add"):
			return "git_add"
		case slices.Contains(args, "commit"):
			return "git_commit"
		case slices.Contains(args, "push"):
			return "git_push"
		}
	case "pg_dump":
		return "pg_dump"
	case "createdb":
		return "createdb"
	case "psql":
		return "psql"
	case "dropdb":
		return "dropdb"
	}
	if slices.Contains(args, "check") {
		return "check_run"
	}
	return "unknown:" + name
}

func newShipTestAgent(t *testing.T, fr *fakeCmdRunner) *Agent {
	t.Helper()
	root := t.TempDir()
	ed, err := codeedit.New(root)
	if err != nil {
		t.Fatalf("codeedit.New: %v", err)
	}
	sc := selfcode.New(fr, root, t.TempDir(), selfcode.DBConfig{Host: "h", Port: "5432", User: "u", Pass: "p", Name: "d"})
	return &Agent{
		codeedit:  ed,
		selfcode:  sc,
		cmdRunner: fr,
		config: &config.Config{
			RickRepoDir:    root,
			WorkDir:        root,
			GitAuthorName:  "doofus-rick",
			GitAuthorEmail: "rick@localhost",
			GitHubToken:    shipTestToken,
		},
	}
}

func codeShipTestTool(a *Agent) (func(context.Context, json.RawMessage) (string, error), bool) {
	event := &events.MessageCreate{GenericMessage: &events.GenericMessage{}}
	tool, ok := a.buildTools(event).Find("code_ship")
	if !ok {
		return nil, false
	}
	return func(ctx context.Context, in json.RawMessage) (string, error) {
		res, err := tool.Execute(ctx, in)
		return res.Content, err
	}, true
}

func TestCodeShipGateFailureAbortsAndDoesNotCommit(t *testing.T) {
	stages := []string{"go_build", "go_vet", "go_test", "go_build_check", "check_run", "pg_dump", "git_status"}
	for _, stage := range stages {
		t.Run(stage, func(t *testing.T) {
			fr := newFakeCmdRunner()
			fr.errs[stage] = errors.New("boom")
			a := newShipTestAgent(t, fr)
			exec, ok := codeShipTestTool(a)
			if !ok {
				t.Fatal("code_ship tool not found")
			}
			_, err := exec(context.Background(), json.RawMessage(`{"message":"m"}`))
			if err == nil {
				t.Fatalf("stage %s: expected error, got nil", stage)
			}
			if fr.called("git_commit") {
				t.Errorf("stage %s: git commit ran despite gate failure", stage)
			}
			if fr.called("git_push") {
				t.Errorf("stage %s: git push ran despite gate failure", stage)
			}
		})
	}
}

func TestCodeShipMigrationVerificationFailureAbortsBeforeCommit(t *testing.T) {
	fr := newFakeCmdRunner()
	fr.results["git_status"] = " M internal/store/migrations/0001_init.sql\n"
	fr.errs["psql"] = errors.New("restore failed")
	a := newShipTestAgent(t, fr)
	exec, ok := codeShipTestTool(a)
	if !ok {
		t.Fatal("code_ship tool not found")
	}
	_, err := exec(context.Background(), json.RawMessage(`{"message":"m"}`))
	if err == nil {
		t.Fatal("expected migration verification failure to abort code_ship")
	}
	if fr.called("git_commit") {
		t.Error("git commit ran despite migration verification failure")
	}
	if fr.called("git_push") {
		t.Error("git push ran despite migration verification failure")
	}
	if !fr.called("dropdb") {
		t.Error("scratch database should be dropped even when verification fails")
	}
}

func TestCodeShipSuccessPathBuildsVetsTestsCommitsAndPushesInline(t *testing.T) {
	fr := newFakeCmdRunner()
	a := newShipTestAgent(t, fr)
	exec, ok := codeShipTestTool(a)
	if !ok {
		t.Fatal("code_ship tool not found")
	}

	content, err := exec(context.Background(), json.RawMessage(`{"message":"ship it"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if content == "" {
		t.Error("expected a non-empty confirmation message")
	}
	for _, stage := range []string{"go_build", "go_vet", "go_test", "go_build_check", "check_run", "git_commit", "git_push"} {
		if !fr.called(stage) {
			t.Errorf("expected stage %s to run on the success path", stage)
		}
	}
	if fr.called("createdb") || fr.called("psql") || fr.called("dropdb") {
		t.Error("migration verification should be skipped when migrations are unchanged")
	}
}

func TestCodeShipPushFailureIsSurfacedAsError(t *testing.T) {
	fr := newFakeCmdRunner()
	fr.errs["git_push"] = errors.New("remote rejected")
	a := newShipTestAgent(t, fr)
	exec, ok := codeShipTestTool(a)
	if !ok {
		t.Fatal("code_ship tool not found")
	}

	content, err := exec(context.Background(), json.RawMessage(`{"message":"m"}`))
	if err == nil {
		t.Fatal("expected push failure to be returned as an error")
	}
	if content != "" {
		t.Errorf("expected no success content on push failure, got %q", content)
	}
	if !fr.called("git_commit") {
		t.Error("expected commit to have already happened before the push was attempted")
	}
}

func TestCodeShipMutexContentionReturnsBusyError(t *testing.T) {
	fr := newFakeCmdRunner()
	a := newShipTestAgent(t, fr)
	a.repoMu.Lock()
	defer a.repoMu.Unlock()

	exec, ok := codeShipTestTool(a)
	if !ok {
		t.Fatal("code_ship tool not found")
	}
	_, err := exec(context.Background(), json.RawMessage(`{"message":"m"}`))
	if !errors.Is(err, errRepoBusy) {
		t.Fatalf("Execute() error = %v, want errRepoBusy", err)
	}
	if len(fr.calls) != 0 {
		t.Errorf("expected no commands run under mutex contention, got %d calls", len(fr.calls))
	}
}

func TestCodeShipPushEnvCarriesTokenArgsDoNot(t *testing.T) {
	fr := newFakeCmdRunner()
	a := newShipTestAgent(t, fr)
	exec, ok := codeShipTestTool(a)
	if !ok {
		t.Fatal("code_ship tool not found")
	}
	if _, err := exec(context.Background(), json.RawMessage(`{"message":"m"}`)); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	call, ok := fr.callFor("git_push")
	if !ok {
		t.Fatal("expected a git push call to be recorded")
	}

	wantEnv := "RICK_PUSH_TOKEN=" + shipTestToken
	found := false
	for _, e := range call.env {
		if e == wantEnv {
			found = true
		}
	}
	if !found {
		t.Errorf("push env %v does not carry the token", call.env)
	}
	for _, arg := range call.args {
		if strings.Contains(arg, shipTestToken) {
			t.Errorf("push argument list leaks the token: %q", arg)
		}
	}
}
