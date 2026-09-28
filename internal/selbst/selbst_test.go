package selbst

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func sampleFacts() Facts {
	return Facts{
		Commit:     "0123456789abcdef0123456789abcdef01234567",
		CommitTime: "2026-09-29T10:00:00Z",
		GoVersion:  "go1.27.1",
		Platform:   "linux/amd64",
		NumCPU:     4,
		Hostname:   "4f2a9c1e7b3d",
		Boot:       time.Date(2026, 9, 29, 10, 5, 0, 0, time.UTC),
		Model:      "anthropic/claude-sonnet-5",
		ShellUser:  "rick",
		PprofAddr:  "127.0.0.1:6060",
		Paths: Paths{
			Work:   "/rick/work",
			Source: "/rick/work/src",
			Logs:   "/rick/work/runtime/logs",
			Crash:  "/rick/work/runtime/crash",
		},
		Deps: map[string]string{
			"github.com/disgoorg/disgo":        "v0.19.6",
			"github.com/OpenRouterTeam/go-sdk": "v0.8.34",
		},
	}
}

func TestBlockIsByteStable(t *testing.T) {
	if a, b := sampleFacts().Block(), sampleFacts().Block(); a != b {
		t.Fatalf("block differs between builds:\n%s\n---\n%s", a, b)
	}
	paths := sampleFacts().Paths
	gather := func() string { return Gather("m", "rick", ":6060", paths).Block() }
	if a, b := gather(), gather(); a != b {
		t.Fatalf("gathered block differs between builds:\n%s\n---\n%s", a, b)
	}
}

func TestBlockContent(t *testing.T) {
	got := sampleFacts().Block()
	t.Log("\n" + got)
	for _, want := range []string{
		"<selbst>\n",
		"commit=0123456\n",
		"commit_full=0123456789abcdef0123456789abcdef01234567\n",
		"commit_time=2026-09-29T10:00:00Z\n",
		"hostname=4f2a9c1e7b3d (container id)\n",
		"boot=2026-09-29T10:05:00Z\n",
		"pprof=http://127.0.0.1:6060/debug/pprof/\n",
		"source=/rick/work/src\n",
		"github.com/disgoorg/disgo=v0.19.6\n",
		"gorm.io/gorm=unknown\n",
		"internal/agent: ",
		"</selbst>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("block missing %q", want)
		}
	}
}

func TestBlockUnsetValues(t *testing.T) {
	f := sampleFacts()
	f.Commit, f.CommitTime, f.PprofAddr = "", "", ""
	got := f.Block()
	for _, want := range []string{"commit=unknown\n", "commit_full=unknown\n", "commit_time=unknown\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("block missing %q", want)
		}
	}
	if strings.Contains(got, "pprof=") {
		t.Error("pprof line should be omitted when the address is empty")
	}
}

func TestLoopbackAddr(t *testing.T) {
	tests := []struct{ in, want string }{
		{":6060", "127.0.0.1:6060"},
		{"127.0.0.1:6060", "127.0.0.1:6060"},
		{"0.0.0.0:6060", "0.0.0.0:6060"},
	}
	for _, tc := range tests {
		if got := loopbackAddr(tc.in); got != tc.want {
			t.Errorf("loopbackAddr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPackageMapMatchesInternal(t *testing.T) {
	var onDisk []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		pkg := filepath.ToSlash(filepath.Dir(path))
		pkg = strings.TrimPrefix(pkg, "../")
		if !slices.Contains(onDisk, pkg) {
			onDisk = append(onDisk, pkg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, pkg := range onDisk {
		if _, ok := packages[pkg]; !ok {
			t.Errorf("internal/%s is missing from the package map", pkg)
		}
	}
	for pkg := range packages {
		if !slices.Contains(onDisk, pkg) {
			t.Errorf("package map names internal/%s, which does not exist", pkg)
		}
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "0h0m"},
		{3*time.Hour + 12*time.Minute + 59*time.Second, "3h12m"},
		{50*time.Hour + 5*time.Minute, "2d2h5m"},
	}
	for _, tc := range tests {
		if got := formatUptime(tc.in); got != tc.want {
			t.Errorf("formatUptime(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestVitals(t *testing.T) {
	got := Vitals(bootTime.Add(3*time.Hour + 12*time.Minute))
	t.Log(got)
	re := regexp.MustCompile(`^<vitals>uptime=3h12m goroutines=[1-9]\d* heap_mb=\d+ gc_cycles=\d+</vitals>$`)
	if !re.MatchString(got) {
		t.Errorf("vitals line %q does not match %s", got, re)
	}
}
