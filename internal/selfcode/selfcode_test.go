package selfcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

type call struct {
	name string
	args []string
	env  []string
}

type fakeRunner struct {
	calls   []call
	results map[string]string
	errs    map[string]error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{results: map[string]string{}, errs: map[string]error{}}
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, env []string) (string, error) {
	f.calls = append(f.calls, call{name: name, args: append([]string(nil), args...), env: env})
	if err, ok := f.errs[name]; ok {
		return f.results[name], err
	}
	return f.results[name], nil
}

func (f *fakeRunner) calledWith(name string) (call, bool) {
	for _, c := range f.calls {
		if c.name == name {
			return c, true
		}
	}
	return call{}, false
}

func (f *fakeRunner) countOf(name string) int {
	n := 0
	for _, c := range f.calls {
		if c.name == name {
			n++
		}
	}
	return n
}

func testSelfcode(runner Runner, repoDir, backupsDir string) *Selfcode {
	return &Selfcode{
		runner:     runner,
		repoDir:    repoDir,
		backupsDir: backupsDir,
		db:         DBConfig{Host: "h", Port: "5432", User: "u", Pass: "secret-pw", Name: "d"},
		now:        time.Now,
		migrate:    func(string) error { return nil },
	}
}

func TestSnapshotPrunesToFiveNewest(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"snapshot-20260101T000000Z.sql",
		"snapshot-20260102T000000Z.sql",
		"snapshot-20260103T000000Z.sql",
		"snapshot-20260104T000000Z.sql",
		"snapshot-20260105T000000Z.sql",
		"snapshot-20260106T000000Z.sql",
		"snapshot-20260107T000000Z.sql",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	// unrelated file must survive pruning untouched.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := pruneSnapshots(dir, snapshotRetention); err != nil {
		t.Fatalf("pruneSnapshots: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var remaining []string
	for _, e := range entries {
		remaining = append(remaining, e.Name())
	}
	sort.Strings(remaining)

	want := []string{
		"notes.txt",
		"snapshot-20260103T000000Z.sql",
		"snapshot-20260104T000000Z.sql",
		"snapshot-20260105T000000Z.sql",
		"snapshot-20260106T000000Z.sql",
		"snapshot-20260107T000000Z.sql",
	}
	if len(remaining) != len(want) {
		t.Fatalf("remaining = %v, want %v", remaining, want)
	}
	for i := range want {
		if remaining[i] != want[i] {
			t.Errorf("remaining[%d] = %q, want %q", i, remaining[i], want[i])
		}
	}
}

func TestSnapshotPruneNoopUnderLimit(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"snapshot-20260101T000000Z.sql", "snapshot-20260102T000000Z.sql"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	if err := pruneSnapshots(dir, snapshotRetention); err != nil {
		t.Fatalf("pruneSnapshots: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
}

func TestSnapshotRunsPgDumpAndPrunes(t *testing.T) {
	dir := t.TempDir()
	runner := newFakeRunner()
	sc := testSelfcode(runner, "/repo", dir)
	sc.now = func() time.Time { return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC) }

	for i := range 6 {
		ts := time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC)
		if err := os.WriteFile(filepath.Join(dir, "snapshot-"+ts.Format("20060102T150405Z")+".sql"), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	path, err := sc.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if want := filepath.Join(dir, "snapshot-20260819T120000Z.sql"); path != want {
		t.Errorf("Snapshot() path = %q, want %q", path, want)
	}

	c, ok := runner.calledWith("pg_dump")
	if !ok {
		t.Fatal("pg_dump not called")
	}
	if !containsArgPair(c.args, "-d", "d") {
		t.Errorf("pg_dump args = %v, want -d d", c.args)
	}
	if !containsArg(c.args, "-w") {
		t.Errorf("pg_dump args = %v, want -w", c.args)
	}
	assertMinimalPGEnv(t, c.args, c.env, "secret-pw")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != snapshotRetention {
		t.Fatalf("len(entries) after Snapshot = %d, want %d", len(entries), snapshotRetention)
	}
}

func TestSnapshotPgDumpFailurePropagates(t *testing.T) {
	dir := t.TempDir()
	runner := newFakeRunner()
	runner.errs["pg_dump"] = errors.New("boom")
	sc := testSelfcode(runner, "/repo", dir)

	if _, err := sc.Snapshot(context.Background()); err == nil {
		t.Fatal("Snapshot() with failing pg_dump: expected error, got nil")
	}
}

func TestMigrationsChanged(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"modified tracked migration", " M internal/store/migrations/0001_init.sql\n", true},
		{"staged modified migration", "M  internal/store/migrations/0001_init.sql\n", true},
		{"non-migration path", " M internal/agent/tools_code.go\n", false},
		{"mixed", " M internal/agent/tools_code.go\n?? internal/store/migrations/0002_foo.sql\n", true},
		{"empty", "", false},
		{"untracked new migration", "?? internal/store/migrations/007_new.sql\n", true},
		{"renamed into migrations", "R  internal/store/old_name.sql -> internal/store/migrations/0003_renamed.sql\n", true},
		{"renamed non-migration", "R  internal/agent/old.go -> internal/agent/new.go\n", false},
		{"quoted untracked migration", "?? \"internal/store/migrations/name with space.sql\"\n", true},
		{"quoted non-migration", "?? \"internal/agent/name with space.go\"\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := newFakeRunner()
			runner.results["git"] = tc.output
			sc := testSelfcode(runner, "/repo", t.TempDir())

			got, err := sc.MigrationsChanged(context.Background())
			if err != nil {
				t.Fatalf("MigrationsChanged: %v", err)
			}
			if got != tc.want {
				t.Errorf("MigrationsChanged() = %v, want %v", got, tc.want)
			}

			c, ok := runner.calledWith("git")
			if !ok {
				t.Fatal("git not called")
			}
			wantArgs := []string{"-C", "/repo", "status", "--porcelain"}
			if !equalArgs(c.args, wantArgs) {
				t.Errorf("git args = %v, want %v", c.args, wantArgs)
			}
		})
	}
}

func TestMigrationsChangedGitFailurePropagates(t *testing.T) {
	runner := newFakeRunner()
	runner.errs["git"] = errors.New("boom")
	sc := testSelfcode(runner, "/repo", t.TempDir())

	if _, err := sc.MigrationsChanged(context.Background()); err == nil {
		t.Fatal("MigrationsChanged() with failing git: expected error, got nil")
	}
}

func TestVerifyMigrationsHappyPath(t *testing.T) {
	runner := newFakeRunner()
	sc := testSelfcode(runner, "/repo", t.TempDir())

	if err := sc.VerifyMigrations(context.Background(), "/backups/snap.sql"); err != nil {
		t.Fatalf("VerifyMigrations: %v", err)
	}
	for _, name := range []string{"createdb", "psql", "dropdb"} {
		if runner.countOf(name) != 1 {
			t.Errorf("%s called %d times, want 1", name, runner.countOf(name))
		}
	}

	for _, name := range []string{"createdb", "psql", "dropdb"} {
		c, _ := runner.calledWith(name)
		if !containsArg(c.args, "-w") {
			t.Errorf("%s args = %v, want -w", name, c.args)
		}
	}

	psql, _ := runner.calledWith("psql")
	for _, want := range []string{"-v", "ON_ERROR_STOP=1", "--single-transaction"} {
		if !containsArg(psql.args, want) {
			t.Errorf("psql args = %v, want %q", psql.args, want)
		}
	}
}

func TestVerifyMigrationsDropsScratchDBOnRestoreFailure(t *testing.T) {
	runner := newFakeRunner()
	runner.errs["psql"] = errors.New("restore failed")
	sc := testSelfcode(runner, "/repo", t.TempDir())

	err := sc.VerifyMigrations(context.Background(), "/backups/snap.sql")
	if err == nil {
		t.Fatal("VerifyMigrations() with failing restore: expected error, got nil")
	}
	if runner.countOf("dropdb") != 1 {
		t.Errorf("dropdb called %d times, want 1", runner.countOf("dropdb"))
	}
	if runner.countOf("psql") != 1 {
		t.Errorf("migrate should not run after restore failure")
	}
}

func TestVerifyMigrationsDropsScratchDBOnMigrateFailure(t *testing.T) {
	runner := newFakeRunner()
	sc := testSelfcode(runner, "/repo", t.TempDir())
	sc.migrate = func(string) error { return errors.New("goose failed") }

	err := sc.VerifyMigrations(context.Background(), "/backups/snap.sql")
	if err == nil {
		t.Fatal("VerifyMigrations() with failing migrate: expected error, got nil")
	}
	if runner.countOf("dropdb") != 1 {
		t.Errorf("dropdb called %d times, want 1", runner.countOf("dropdb"))
	}
}

func TestVerifyMigrationsSkipsDropWhenCreateFails(t *testing.T) {
	runner := newFakeRunner()
	runner.errs["createdb"] = errors.New("create failed")
	sc := testSelfcode(runner, "/repo", t.TempDir())

	err := sc.VerifyMigrations(context.Background(), "/backups/snap.sql")
	if err == nil {
		t.Fatal("VerifyMigrations() with failing createdb: expected error, got nil")
	}
	if runner.countOf("dropdb") != 0 {
		t.Errorf("dropdb called %d times, want 0 since scratch db never created", runner.countOf("dropdb"))
	}
}

func TestVerifyMigrationsPasswordNeverInArgs(t *testing.T) {
	runner := newFakeRunner()
	sc := testSelfcode(runner, "/repo", t.TempDir())

	if err := sc.VerifyMigrations(context.Background(), "/backups/snap.sql"); err != nil {
		t.Fatalf("VerifyMigrations: %v", err)
	}
	for _, name := range []string{"createdb", "psql", "dropdb"} {
		c, ok := runner.calledWith(name)
		if !ok {
			t.Fatalf("%s not called", name)
		}
		assertMinimalPGEnv(t, c.args, c.env, "secret-pw")
	}
}

func TestWithEnvOverridesWithoutDuplicating(t *testing.T) {
	base := []string{"PATH=/bin", "PGPASSWORD=old", "HOME=/root"}
	got := withEnv(base, map[string]string{"PGPASSWORD": "new"})

	count := 0
	found := false
	for _, e := range got {
		if strings.HasPrefix(e, "PGPASSWORD=") {
			count++
			if e == "PGPASSWORD=new" {
				found = true
			}
		}
	}
	if count != 1 {
		t.Fatalf("PGPASSWORD entries = %d, want 1", count)
	}
	if !found {
		t.Errorf("withEnv() = %v, want PGPASSWORD=new present", got)
	}
	if !equalArgs(filterPrefix(got, "PATH="), []string{"PATH=/bin"}) {
		t.Errorf("withEnv() dropped unrelated entries: %v", got)
	}
}

func assertMinimalPGEnv(t *testing.T, args, env []string, password string) {
	t.Helper()
	for _, a := range args {
		if strings.Contains(a, password) {
			t.Errorf("password leaked into argument list: %v", args)
		}
	}
	found := false
	keys := map[string]bool{}
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		keys[key] = true
		if e == "PGPASSWORD="+password {
			found = true
		}
	}
	if !found {
		t.Errorf("env = %v, want PGPASSWORD=%s", env, password)
	}
	for k := range keys {
		if k != "PATH" && k != "PGPASSWORD" {
			t.Errorf("env = %v, want only PATH and PGPASSWORD, found unrelated key %q", env, k)
		}
	}
}

func containsArgPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func filterPrefix(entries []string, prefix string) []string {
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}
