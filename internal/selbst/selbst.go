package selbst

import (
	"fmt"
	"maps"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"
)

// Set via -ldflags -X at image build time; empty in local builds.
var (
	commit     string
	commitTime string
)

var bootTime = time.Now()

const unknown = "unknown"

var keyDeps = []string{
	"github.com/disgoorg/disgo",
	"github.com/OpenRouterTeam/go-sdk",
	"gorm.io/gorm",
	"gorm.io/driver/postgres",
	"github.com/jackc/pgx/v5",
	"github.com/pressly/goose/v3",
}

type Paths struct {
	Work   string
	Source string
	Logs   string
	Crash  string
}

type Facts struct {
	Commit     string
	CommitTime string
	GoVersion  string
	Platform   string
	NumCPU     int
	Hostname   string
	Boot       time.Time
	Model      string
	ShellUser  string
	PprofAddr  string
	Paths      Paths
	Deps       map[string]string
}

func Gather(model, shellUser, pprofAddr string, paths Paths) Facts {
	hostname, _ := os.Hostname()
	return Facts{
		Commit:     commit,
		CommitTime: commitTime,
		GoVersion:  runtime.Version(),
		Platform:   runtime.GOOS + "/" + runtime.GOARCH,
		NumCPU:     runtime.NumCPU(),
		Hostname:   hostname,
		Boot:       bootTime,
		Model:      model,
		ShellUser:  shellUser,
		PprofAddr:  pprofAddr,
		Paths:      paths,
		Deps:       linkedDeps(),
	}
}

func linkedDeps() map[string]string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	deps := make(map[string]string, len(info.Deps))
	for _, d := range info.Deps {
		if d.Replace != nil {
			d = d.Replace
		}
		deps[d.Path] = d.Version
	}
	return deps
}

func (f Facts) Block() string {
	var sb strings.Builder
	sb.WriteString("<selbst>\n")

	fmt.Fprintf(&sb, "commit=%s\n", orUnknown(shortCommit(f.Commit)))
	fmt.Fprintf(&sb, "commit_full=%s\n", orUnknown(f.Commit))
	fmt.Fprintf(&sb, "commit_time=%s\n", orUnknown(f.CommitTime))
	fmt.Fprintf(&sb, "go=%s\n", f.GoVersion)
	fmt.Fprintf(&sb, "platform=%s\n", f.Platform)
	sb.WriteByte('\n')

	fmt.Fprintf(&sb, "hostname=%s (container id)\n", orUnknown(f.Hostname))
	fmt.Fprintf(&sb, "cpus=%d\n", f.NumCPU)
	fmt.Fprintf(&sb, "boot=%s\n", f.Boot.UTC().Format(time.RFC3339))
	fmt.Fprintf(&sb, "model=%s\n", f.Model)
	fmt.Fprintf(&sb, "shell_user=%s\n", f.ShellUser)
	if f.PprofAddr != "" {
		fmt.Fprintf(&sb, "pprof=http://%s/debug/pprof/\n", loopbackAddr(f.PprofAddr))
	}
	sb.WriteByte('\n')

	fmt.Fprintf(&sb, "work_dir=%s\n", f.Paths.Work)
	fmt.Fprintf(&sb, "source=%s\n", f.Paths.Source)
	fmt.Fprintf(&sb, "logs=%s\n", f.Paths.Logs)
	fmt.Fprintf(&sb, "crash=%s\n", f.Paths.Crash)
	sb.WriteByte('\n')

	for _, path := range keyDeps {
		fmt.Fprintf(&sb, "%s=%s\n", path, orUnknown(f.Deps[path]))
	}
	sb.WriteByte('\n')

	for _, name := range slices.Sorted(maps.Keys(packages)) {
		fmt.Fprintf(&sb, "internal/%s: %s\n", name, packages[name])
	}

	sb.WriteString("</selbst>")
	return sb.String()
}

func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

func loopbackAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	return addr
}

func orUnknown(s string) string {
	if s == "" {
		return unknown
	}
	return s
}
