package runtimehome

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	maxProblems     = 50
	maxLogLineBytes = 1 << 20
)

type LogAttr struct {
	Key   string
	Value string
}

type LogEntry struct {
	Time  time.Time
	Level slog.Level
	Msg   string
	Attrs []LogAttr
}

type CrashReport struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// Problems returns the entries it could read even when err is non-nil.
func (h *Home) Problems(since time.Time) ([]LogEntry, error) {
	entries, err := readProblems(h.logsDir, since, time.Now())
	if err != nil {
		return entries, fmt.Errorf("read problem logs: %w", err)
	}
	return entries, nil
}

func (h *Home) CrashReports() ([]CrashReport, error) {
	reports, err := listCrashReports(h.crashDir)
	if err != nil {
		return reports, fmt.Errorf("list crash reports: %w", err)
	}
	return reports, nil
}

func readProblems(logsDir string, since, now time.Time) ([]LogEntry, error) {
	var entries []LogEntry
	var errs []error
	for day := dayStart(since); !day.After(now); day = day.Add(24 * time.Hour) {
		found, err := readProblemsFile(filepath.Join(logsDir, logFileName(day)), since)
		if err != nil {
			errs = append(errs, err)
		}
		entries = append(entries, found...)
	}
	slices.SortStableFunc(entries, func(a, b LogEntry) int { return a.Time.Compare(b.Time) })
	if len(entries) > maxProblems {
		entries = entries[len(entries)-maxProblems:]
	}
	return entries, errors.Join(errs...)
}

func readProblemsFile(path string, since time.Time) ([]LogEntry, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []LogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLogLineBytes)
	for sc.Scan() {
		entry, ok := parseLogLine(sc.Bytes())
		if !ok || entry.Level < slog.LevelWarn || entry.Time.Before(since) {
			continue
		}
		entries = append(entries, entry)
	}
	if err := sc.Err(); err != nil {
		return entries, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return entries, nil
}

func parseLogLine(line []byte) (LogEntry, bool) {
	var entry LogEntry
	var hasTime, hasLevel bool
	err := walkObject(line, "", func(key string, raw json.RawMessage) error {
		switch key {
		case slog.TimeKey:
			hasTime = json.Unmarshal(raw, &entry.Time) == nil
		case slog.LevelKey:
			var s string
			hasLevel = json.Unmarshal(raw, &s) == nil && entry.Level.UnmarshalText([]byte(s)) == nil
		case slog.MessageKey:
			return json.Unmarshal(raw, &entry.Msg)
		default:
			entry.Attrs = append(entry.Attrs, LogAttr{Key: key, Value: rawValue(raw)})
		}
		return nil
	})
	return entry, err == nil && hasTime && hasLevel
}

func walkObject(data []byte, prefix string, visit func(key string, raw json.RawMessage) error) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fmt.Errorf("not a json object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if len(raw) > 0 && raw[0] == '{' {
			err = walkObject(raw, prefix+key+".", visit)
		} else {
			err = visit(prefix+key, raw)
		}
		if err != nil {
			return err
		}
	}
	_, err := dec.Token()
	return err
}

func rawValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

func listCrashReports(crashDir string) ([]CrashReport, error) {
	entries, err := os.ReadDir(crashDir)
	if err != nil {
		return nil, err
	}
	var reports []CrashReport
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, crashPrefix) || !strings.HasSuffix(name, crashSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if info.Size() == 0 {
			continue
		}
		reports = append(reports, CrashReport{Path: filepath.Join(crashDir, name), Size: info.Size(), ModTime: info.ModTime()})
	}
	slices.SortFunc(reports, func(a, b CrashReport) int { return a.ModTime.Compare(b.ModTime) })
	return reports, errors.Join(errs...)
}
