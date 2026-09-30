package runtimehome

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadDeploysSkipsMalformedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), deploysFile)
	at := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	ship := DeployRecord{Kind: DeployShip, Commit: "abc", ChannelID: "1", Requester: "2", Summary: "fix", At: at}
	if err := AppendDeploy(path, ship); err != nil {
		t.Fatalf("AppendDeploy: %v", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not json\n{\"commit\":\"no kind\"}\n\n{\"kind\":\"boot\",\"at\":\"garbage\"}\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	boot := DeployRecord{Kind: DeployBoot, Commit: "abc", CrashFile: "/c/boot-1.txt", At: at.Add(time.Minute)}
	if err := AppendDeploy(path, boot); err != nil {
		t.Fatalf("AppendDeploy: %v", err)
	}

	got, err := ReadDeploys(path)
	if err != nil {
		t.Fatalf("ReadDeploys: %v", err)
	}
	if len(got) != 2 || got[0] != ship || got[1] != boot {
		t.Fatalf("ReadDeploys = %+v, want [%+v %+v]", got, ship, boot)
	}
}

func TestReadDeploysMissingFileIsEmpty(t *testing.T) {
	got, err := ReadDeploys(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil || len(got) != 0 {
		t.Fatalf("ReadDeploys = %v, %v; want empty, nil", got, err)
	}
}

func TestJournalCachesAndTracksAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), deploysFile)
	j := NewJournal(path)
	first := DeployRecord{Kind: DeployShip, Commit: "a", At: time.Unix(1, 0).UTC()}
	if err := j.Append(first); err != nil {
		t.Fatal(err)
	}
	got, err := j.Records()
	if err != nil || len(got) != 1 {
		t.Fatalf("Records = %v, %v", got, err)
	}

	if err := AppendDeploy(path, DeployRecord{Kind: DeployBoot, At: time.Unix(2, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	second := DeployRecord{Kind: DeployReported, Commit: "a", At: time.Unix(3, 0).UTC()}
	if err := j.Append(second); err != nil {
		t.Fatal(err)
	}
	got, err = j.Records()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1] != second {
		t.Fatalf("cached Records = %+v, want first and second only", got)
	}

	got[0].Commit = "mutated"
	again, _ := j.Records()
	if again[0].Commit != "a" {
		t.Error("Records must return a copy")
	}
}
