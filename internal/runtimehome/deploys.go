package runtimehome

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sync"
	"time"
)

const deploysFile = "deploys.jsonl"

type DeployKind string

const (
	DeployShip     DeployKind = "ship"
	DeployBoot     DeployKind = "boot"
	DeployReported DeployKind = "reported"
)

type DeployRecord struct {
	Kind      DeployKind `json:"kind"`
	Commit    string     `json:"commit,omitempty"`
	ChannelID string     `json:"channel_id,omitempty"`
	Requester string     `json:"requester,omitempty"`
	Summary   string     `json:"summary,omitempty"`
	CrashFile string     `json:"crash_file,omitempty"`
	At        time.Time  `json:"at"`
}

func AppendDeploy(path string, r DeployRecord) error {
	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode deploy record: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open deploy journal: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("append deploy journal: %w", err)
	}
	return f.Close()
}

func ReadDeploys(path string) ([]DeployRecord, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open deploy journal: %w", err)
	}
	defer func() { _ = f.Close() }()

	var records []DeployRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLogLineBytes)
	for sc.Scan() {
		var r DeployRecord
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.Kind == "" {
			continue
		}
		records = append(records, r)
	}
	if err := sc.Err(); err != nil {
		return records, fmt.Errorf("read deploy journal: %w", err)
	}
	return records, nil
}

type Journal struct {
	path    string
	mu      sync.Mutex
	loaded  bool
	records []DeployRecord
}

func NewJournal(path string) *Journal {
	return &Journal{path: path}
}

func (j *Journal) Path() string {
	return j.path
}

func (j *Journal) Append(r DeployRecord) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := AppendDeploy(j.path, r); err != nil {
		return err
	}
	if j.loaded {
		j.records = append(j.records, r)
	}
	return nil
}

// Records reads the file once and serves later calls from memory, so this process must be the only writer.
func (j *Journal) Records() ([]DeployRecord, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.loaded {
		records, err := ReadDeploys(j.path)
		if err != nil {
			return records, err
		}
		j.records, j.loaded = records, true
	}
	return slices.Clone(j.records), nil
}
