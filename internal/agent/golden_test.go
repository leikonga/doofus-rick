package agent

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite golden files")

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(want) != got {
		t.Errorf("%s mismatch\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}

func TestBuildCachedPrefixGolden(t *testing.T) {
	tests := []struct {
		name                                          string
		selbst, roster, channelID, channelName, topic string
	}{
		{"all_blocks", "SELBST BLOCK", "ROSTER BLOCK", "1234567890", "general", "the topic"},
		{"empty_optional_blocks", "", "", "1234567890", "general", "the topic"},
		{"no_selbst", "", "ROSTER BLOCK", "1234567890", "general", "the topic"},
		{"no_roster", "SELBST BLOCK", "", "1234567890", "general", "the topic"},
		{"no_topic", "SELBST BLOCK", "ROSTER BLOCK", "1234567890", "general", ""},
		{"no_channel", "SELBST BLOCK", "ROSTER BLOCK", "1234567890", "", "ignored topic"},
		{"nothing", "", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCachedPrefix(tt.selbst, tt.roster, tt.channelID, tt.channelName, tt.topic)
			checkGolden(t, "cached_prefix_"+tt.name+".golden", got)
		})
	}
}

func TestBuildVolatileTurnGolden(t *testing.T) {
	now := time.Date(2026, 3, 14, 15, 9, 0, 0, time.FixedZone("CET", 3600))
	history := []string{"[15:01 alice]: hallo", "[15:02 rick (du)]: servus"}

	tests := []struct {
		name                            string
		vitals, gradDo, recall, trigger string
		history                         []string
	}{
		{"all_blocks", "<vitals>ok</vitals>", "<grad>roster</grad>\n\n", "<recall>memo</recall>\n", "[15:09 alice]: was geht", history},
		{"empty_optional_blocks", "", "", "", "(pinged Rick)", nil},
		{"history_only", "", "", "", "[15:09 alice]: hi", history},
		{"vitals_and_recall", "<vitals>ok</vitals>", "", "<recall>memo</recall>", "[15:09 alice]: hi", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildVolatileTurn(now, tt.vitals, tt.gradDo, tt.recall, tt.history, tt.trigger)
			checkGolden(t, "volatile_turn_"+tt.name+".golden", got)
		})
	}
}

func TestBuildToolsGolden(t *testing.T) {
	a := &Agent{shellDesc: "FIXED SHELL DESCRIPTION"}
	tools := a.buildTools(turnOrigin{})

	type toolSpec struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Schema      map[string]any `json:"schema"`
	}
	specs := make([]toolSpec, 0, len(tools))
	for _, tool := range tools {
		specs = append(specs, toolSpec{tool.Name, tool.Description, tool.Schema})
	}
	out, err := json.MarshalIndent(specs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "tools.golden", string(out)+"\n")
}
