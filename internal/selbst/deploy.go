package selbst

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/leikonga/doofus-rick/internal/runtimehome"
)

const (
	maxExcerptBytes   = 1500
	crashHeadBytes    = 64 << 10
	maxSummaryInFacts = 200
	truncatedMarker   = "\n(truncated)"
)

type DeployOutcome string

const (
	DeploySucceeded DeployOutcome = "success"
	DeployCrashed   DeployOutcome = "crash"
)

type Announcement struct {
	Outcome       DeployOutcome
	Ship          runtimehome.DeployRecord
	Running       string
	CrashFile     string
	CrashedCommit string
}

func Commit() string {
	return commit
}

// EvaluateBoot allows at most one success and one crash announcement per shipped commit, crash first.
func EvaluateBoot(records []runtimehome.DeployRecord, running string, crashes []runtimehome.CrashReport, currentCrash string) (Announcement, bool) {
	ship, ok := latestShip(records)
	if !ok {
		return Announcement{}, false
	}

	var shipReported, crashReported bool
	reportedFiles := map[string]bool{}
	for _, r := range records {
		if r.Kind != runtimehome.DeployReported {
			continue
		}
		if r.CrashFile != "" {
			reportedFiles[r.CrashFile] = true
		}
		if r.Commit == ship.Commit {
			shipReported = true
			crashReported = crashReported || r.CrashFile != ""
		}
	}

	if !crashReported {
		var latest *runtimehome.CrashReport
		for i, c := range crashes {
			if c.Path == currentCrash || reportedFiles[c.Path] || c.ModTime.Before(ship.At) {
				continue
			}
			if latest == nil || c.ModTime.After(latest.ModTime) {
				latest = &crashes[i]
			}
		}
		if latest != nil {
			return Announcement{
				Outcome:       DeployCrashed,
				Ship:          ship,
				Running:       running,
				CrashFile:     latest.Path,
				CrashedCommit: bootCommit(records, latest.Path),
			}, true
		}
	}

	if running != "" && running == ship.Commit && !shipReported {
		return Announcement{Outcome: DeploySucceeded, Ship: ship, Running: running}, true
	}
	return Announcement{}, false
}

func latestShip(records []runtimehome.DeployRecord) (runtimehome.DeployRecord, bool) {
	for _, record := range slices.Backward(records) {
		if record.Kind == runtimehome.DeployShip {
			return record, true
		}
	}
	return runtimehome.DeployRecord{}, false
}

func bootCommit(records []runtimehome.DeployRecord, crashFile string) string {
	for _, r := range records {
		if r.Kind == runtimehome.DeployBoot && r.CrashFile == crashFile {
			return r.Commit
		}
	}
	return ""
}

func (a Announcement) Record(at time.Time) runtimehome.DeployRecord {
	return runtimehome.DeployRecord{Kind: runtimehome.DeployReported, Commit: a.Ship.Commit, CrashFile: a.CrashFile, At: at}
}

func (a Announcement) Facts(excerpt string) string {
	shipped := fmt.Sprintf("commit %s (%q) that you shipped for <@%s>", shortCommit(a.Ship.Commit), summaryLine(a.Ship.Summary), a.Ship.Requester)
	if a.Outcome == DeploySucceeded {
		return fmt.Sprintf("%s is now running. the deploy worked.", shipped)
	}
	crashed := orUnknown(shortCommit(a.CrashedCommit))
	var sb strings.Builder
	fmt.Fprintf(&sb, "a boot running commit %s crashed after %s. now running: %s.", crashed, shipped, orUnknown(shortCommit(a.Running)))
	if excerpt != "" {
		fmt.Fprintf(&sb, " the excerpt below gets posted right after your message, do not repeat it.\ncrash excerpt:\n%s", excerpt)
	}
	return sb.String()
}

func summaryLine(summary string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(summary), "\n")
	return capBytes(line, maxSummaryInFacts)
}

func ReadCrashExcerpt(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head, err := io.ReadAll(io.LimitReader(f, crashHeadBytes))
	if err != nil {
		return "", err
	}
	return CrashExcerpt(string(head)), nil
}

func CrashExcerpt(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	start := 0
	for i, line := range lines {
		if strings.HasPrefix(line, "panic: ") || strings.HasPrefix(line, "fatal error: ") {
			start = i
			break
		}
	}

	var sb strings.Builder
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if sb.Len() == 0 && len(line) > maxExcerptBytes {
			return capBytes(line, maxExcerptBytes-len(truncatedMarker)) + truncatedMarker
		}
		if sb.Len()+len(line)+1 > maxExcerptBytes-len(truncatedMarker) {
			sb.WriteString(strings.TrimPrefix(truncatedMarker, "\n"))
			return sb.String()
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

func capBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func DeployStatus(records []runtimehome.DeployRecord, running string, now time.Time) string {
	ship, ok := latestShip(records)
	switch {
	case !ok:
		return "none"
	case running == "":
		return "unknown"
	case running == ship.Commit:
		return "ok"
	default:
		return fmt.Sprintf("pending:%s:%s", shortCommit(ship.Commit), formatAge(now.Sub(ship.At)))
	}
}

func formatAge(d time.Duration) string {
	minutes := max(int(d/time.Minute), 0)
	days, hours, mins := minutes/(24*60), minutes/60%24, minutes%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
