package runtimehome

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

const (
	logRetention = 14 * 24 * time.Hour
	logPrefix    = "rick-"
	logSuffix    = ".jsonl"
	dayLayout    = "2006-01-02"
	crashPrefix  = "boot-"
	crashSuffix  = ".txt"
)

type Home struct {
	logs *dailyLog
}

func Open(workDir string, now time.Time) (*Home, error) {
	logsDir, crashDir, err := ensureLayout(workDir)
	if err != nil {
		return nil, err
	}

	if err := setCrashOutput(crashDir, now); err != nil {
		return nil, err
	}

	logs := &dailyLog{dir: logsDir}
	if err := logs.rotate(now); err != nil {
		return nil, err
	}

	if err := pruneLogs(logsDir, now); err != nil {
		slog.Warn("failed to prune old log files", "dir", logsDir, "error", err)
	}

	return &Home{logs: logs}, nil
}

func (h *Home) Handler() slog.Handler {
	return newDailyHandler(h.logs)
}

func (h *Home) Close() error {
	return h.logs.close()
}

func ensureLayout(workDir string) (logsDir, crashDir string, err error) {
	root := filepath.Join(workDir, "runtime")
	logsDir = filepath.Join(root, "logs")
	crashDir = filepath.Join(root, "crash")
	for _, dir := range []string{logsDir, crashDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", "", fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return logsDir, crashDir, nil
}

func setCrashOutput(crashDir string, now time.Time) error {
	name := fmt.Sprintf("%s%d%s", crashPrefix, now.Unix(), crashSuffix)
	if err := removeEmptyCrashFiles(crashDir, name); err != nil {
		slog.Warn("failed to remove empty crash files", "dir", crashDir, "error", err)
	}

	f, err := os.OpenFile(filepath.Join(crashDir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open crash file: %w", err)
	}
	defer f.Close()
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		return fmt.Errorf("set crash output: %w", err)
	}
	return nil
}

func removeEmptyCrashFiles(crashDir, current string) error {
	entries, err := os.ReadDir(crashDir)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if name == current || e.IsDir() || !strings.HasPrefix(name, crashPrefix) || !strings.HasSuffix(name, crashSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if info.Size() > 0 {
			continue
		}
		if err := os.Remove(filepath.Join(crashDir, name)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func pruneLogs(logsDir string, now time.Time) error {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return err
	}
	cutoff := dayStart(now).Add(-logRetention)
	var errs []error
	for _, e := range entries {
		day, ok := logFileDay(e.Name())
		if !ok || e.IsDir() || !day.Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(logsDir, e.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func logFileName(t time.Time) string {
	return logPrefix + t.UTC().Format(dayLayout) + logSuffix
}

func logFileDay(name string) (time.Time, bool) {
	datePart, ok := strings.CutPrefix(name, logPrefix)
	if !ok {
		return time.Time{}, false
	}
	datePart, ok = strings.CutSuffix(datePart, logSuffix)
	if !ok {
		return time.Time{}, false
	}
	day, err := time.Parse(dayLayout, datePart)
	return day, err == nil
}

func dayStart(t time.Time) time.Time {
	return t.UTC().Truncate(24 * time.Hour)
}

type dailyLog struct {
	mu   sync.Mutex
	dir  string
	name string
	file *os.File
}

func (d *dailyLog) rotate(t time.Time) error {
	name := logFileName(t)
	if d.file != nil && name == d.name {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(d.dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	if d.file != nil {
		d.file.Close()
	}
	d.file, d.name = f, name
	return nil
}

func (d *dailyLog) Write(p []byte) (int, error) {
	return d.file.Write(p)
}

func (d *dailyLog) close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	return err
}

type dailyHandler struct {
	log   *dailyLog
	inner slog.Handler
}

func newDailyHandler(log *dailyLog) *dailyHandler {
	return &dailyHandler{log: log, inner: slog.NewJSONHandler(log, nil)}
}

func (h *dailyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *dailyHandler) Handle(ctx context.Context, r slog.Record) error {
	h.log.mu.Lock()
	defer h.log.mu.Unlock()
	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}
	if err := h.log.rotate(t); err != nil {
		return err
	}
	return h.inner.Handle(ctx, r)
}

func (h *dailyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &dailyHandler{log: h.log, inner: h.inner.WithAttrs(attrs)}
}

func (h *dailyHandler) WithGroup(name string) slog.Handler {
	return &dailyHandler{log: h.log, inner: h.inner.WithGroup(name)}
}
