package selfcode

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/leikonga/doofus-rick/internal/store"
)

const (
	snapshotPrefix    = "snapshot-"
	snapshotSuffix    = ".sql"
	snapshotRetention = 5
	migrationsPrefix  = "internal/store/migrations/"
)

// Runner executes a single external command. The real implementation
// wraps os/exec; tests substitute a fake.
type Runner interface {
	Run(ctx context.Context, name string, args []string, env []string) (string, error)
}

// DBConfig is the connection information needed by the postgres client
// tools (pg_dump, psql, createdb, dropdb).
type DBConfig struct {
	Host string
	Port string
	User string
	Pass string
	Name string
}

type Selfcode struct {
	runner     Runner
	repoDir    string
	backupsDir string
	db         DBConfig
	now        func() time.Time
	migrate    func(dsn string) error
}

func New(runner Runner, repoDir, backupsDir string, db DBConfig) *Selfcode {
	return &Selfcode{
		runner:     runner,
		repoDir:    repoDir,
		backupsDir: backupsDir,
		db:         db,
		now:        time.Now,
		migrate:    store.RunMigrationsDSN,
	}
}

// Snapshot pg_dumps the configured database into the backups directory
// with a timestamped filename, then prunes to the newest 5 snapshots.
func (s *Selfcode) Snapshot(ctx context.Context) (string, error) {
	if err := os.MkdirAll(s.backupsDir, 0o755); err != nil {
		return "", fmt.Errorf("create backups dir %q: %w", s.backupsDir, err)
	}

	name := snapshotPrefix + s.now().UTC().Format("20060102T150405Z") + snapshotSuffix
	path := filepath.Join(s.backupsDir, name)

	args := []string{"-h", s.db.Host, "-p", s.db.Port, "-U", s.db.User, "-w", "-d", s.db.Name, "-f", path}
	if _, err := s.runner.Run(ctx, "pg_dump", args, s.pgEnv()); err != nil {
		return "", fmt.Errorf("pg_dump: %w", err)
	}

	if err := pruneSnapshots(s.backupsDir, snapshotRetention); err != nil {
		return "", fmt.Errorf("prune snapshots: %w", err)
	}

	return path, nil
}

// pruneSnapshots keeps the newest n snapshot files in dir. Filenames embed
// a zero-padded UTC timestamp, so lexical order is chronological order.
func pruneSnapshots(dir string, n int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), snapshotPrefix) || !strings.HasSuffix(e.Name(), snapshotSuffix) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	if len(names) <= n {
		return nil
	}
	for _, name := range names[:len(names)-n] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// MigrationsChanged reports whether internal/store/migrations/ changed.
// Uses status, not diff --name-only, which misses untracked new files.
func (s *Selfcode) MigrationsChanged(ctx context.Context) (bool, error) {
	out, err := s.runner.Run(ctx, "git", []string{"-C", s.repoDir, "status", "--porcelain"}, nil)
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if len(line) < 4 {
			continue
		}
		for path := range strings.SplitSeq(line[3:], " -> ") {
			if strings.HasPrefix(unquotePath(path), migrationsPrefix) {
				return true, nil
			}
		}
	}
	return false, nil
}

// git quotes paths containing spaces or non-ASCII characters.
func unquotePath(p string) string {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, `"`) {
		return p
	}
	if unquoted, err := strconv.Unquote(p); err == nil {
		return unquoted
	}
	return strings.Trim(p, `"`)
}

// VerifyMigrations restores snapshotPath into a scratch database and runs
// the embedded migrations against it, dropping the scratch db even on failure.
func (s *Selfcode) VerifyMigrations(ctx context.Context, snapshotPath string) error {
	scratch := fmt.Sprintf("rick_verify_%d", s.now().UnixNano())
	env := s.pgEnv()

	createArgs := []string{"-h", s.db.Host, "-p", s.db.Port, "-U", s.db.User, "-w", scratch}
	if _, err := s.runner.Run(ctx, "createdb", createArgs, env); err != nil {
		return fmt.Errorf("create scratch database %s: %w", scratch, err)
	}
	defer func() {
		dropArgs := []string{"-h", s.db.Host, "-p", s.db.Port, "-U", s.db.User, "-w", scratch}
		if _, err := s.runner.Run(context.Background(), "dropdb", dropArgs, env); err != nil {
			slog.Error("drop scratch database", "database", scratch, "error", err)
		}
	}()

	restoreArgs := []string{
		"-h", s.db.Host, "-p", s.db.Port, "-U", s.db.User, "-w",
		"-v", "ON_ERROR_STOP=1", "--single-transaction",
		"-d", scratch, "-f", snapshotPath,
	}
	if _, err := s.runner.Run(ctx, "psql", restoreArgs, env); err != nil {
		return fmt.Errorf("restore snapshot into %s: %w", scratch, err)
	}

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		s.db.Host, s.db.User, s.db.Pass, scratch, s.db.Port)
	if err := s.migrate(dsn); err != nil {
		return fmt.Errorf("run migrations against %s: %w", scratch, err)
	}

	return nil
}

func (s *Selfcode) pgEnv() []string {
	return withEnv([]string{"PATH=" + os.Getenv("PATH")}, map[string]string{"PGPASSWORD": s.db.Pass})
}

// withEnv returns base with the given overrides applied, replacing any
// existing entries for the same keys rather than appending duplicates.
func withEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	for _, e := range base {
		key, _, ok := strings.Cut(e, "=")
		if ok {
			if _, overridden := overrides[key]; overridden {
				continue
			}
		}
		out = append(out, e)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}
