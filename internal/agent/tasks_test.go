package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/store"
)

type fakeTaskStore struct {
	mu          sync.Mutex
	tasks       []store.Task
	interrupted []store.Task
}

func (f *fakeTaskStore) CreateTask(_ context.Context, t store.Task) (store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t.ID = uint64(len(f.tasks) + 1)
	t.Status = store.TaskPending
	f.tasks = append(f.tasks, t)
	return t, nil
}

func (f *fakeTaskStore) ListTasks(context.Context, int) ([]store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Task(nil), f.tasks...), nil
}

func (f *fakeTaskStore) CancelTask(_ context.Context, id uint64) (store.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, t := range f.tasks {
		if t.ID != id {
			continue
		}
		if !t.Status.Active() {
			return t, store.ErrTaskFinished
		}
		f.tasks[i].Status = store.TaskCancelled
		return t, nil
	}
	return store.Task{}, store.ErrTaskNotFound
}

func (f *fakeTaskStore) ClaimDueTasks(context.Context, time.Time) ([]store.Task, error) {
	return nil, nil
}

func (f *fakeTaskStore) FinishTask(context.Context, uint64, store.TaskStatus, string) error {
	return nil
}

func (f *fakeTaskStore) InterruptRunningTasks(context.Context) ([]store.Task, error) {
	return f.interrupted, nil
}

func newTaskTestScheduler(fs *fakeTaskStore) *Scheduler {
	a := &Agent{discord: &mockDiscord{users: map[string]string{"100": "alice", "200": "bob"}}}
	return newScheduler(fs, time.Minute, nil, nil, a.userName)
}

func execTaskTool(t *testing.T, s *Scheduler, origin turnOrigin, in string) (string, error) {
	t.Helper()
	tool, ok := (&Agent{tasks: s}).buildTools(origin).Find("sys_task")
	if !ok {
		t.Fatal("sys_task tool not found")
	}
	res, err := tool.Execute(context.Background(), json.RawMessage(in))
	return res.Content(), err
}

func TestTaskCreateRejectedUnderTaskOrigin(t *testing.T) {
	fs := &fakeTaskStore{}
	s := newTaskTestScheduler(fs)
	_, err := execTaskTool(t, s, turnOrigin{ChannelID: 1, AuthorID: 100, TaskID: 7}, `{"command":"create","prompt":"x"}`)
	if !errors.Is(err, errTaskFromTask) {
		t.Fatalf("create under task origin: err = %v, want errTaskFromTask", err)
	}
	if len(fs.tasks) != 0 {
		t.Errorf("task stored despite rejection: %+v", fs.tasks)
	}
}

func TestTaskCreateStoresOrigin(t *testing.T) {
	fs := &fakeTaskStore{}
	s := newTaskTestScheduler(fs)
	out, err := execTaskTool(t, s, turnOrigin{ChannelID: 42, AuthorID: 100, MessageID: 9}, `{"command":"create","prompt":"remind alice","in":"2h"}`)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(out, "task #1") {
		t.Errorf("create result = %q, want task id", out)
	}
	got := fs.tasks[0]
	if got.ChannelID != 42 || got.RequesterID != 100 || got.Prompt != "remind alice" {
		t.Errorf("stored task = %+v", got)
	}
}

func TestTaskFireAt(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		fireAt  string
		in      string
		want    time.Time
		wantErr bool
	}{
		{name: "neither means now", want: now},
		{name: "in duration", in: "2h30m", want: now.Add(150 * time.Minute)},
		{name: "fire_at with offset", fireAt: "2026-09-29T20:00:00+02:00", want: time.Date(2026, 9, 29, 18, 0, 0, 0, time.UTC)},
		{name: "fire_at utc", fireAt: "2026-09-29T13:00:00Z", want: now.Add(time.Hour)},
		{name: "both set", fireAt: "2026-09-29T13:00:00Z", in: "1h", wantErr: true},
		{name: "fire_at without offset", fireAt: "2026-09-29T13:00:00", wantErr: true},
		{name: "fire_at in past", fireAt: "2026-09-29T11:00:00Z", wantErr: true},
		{name: "bad duration", in: "2 days", wantErr: true},
		{name: "negative duration", in: "-1h", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := taskFireAt(now, tt.fireAt, tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("taskFireAt() err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !got.Equal(tt.want) {
				t.Errorf("taskFireAt() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestTaskListAndCancelAcrossRequesters(t *testing.T) {
	fs := &fakeTaskStore{tasks: []store.Task{
		{ID: 1, ChannelID: 1, RequesterID: 100, Prompt: "alice task", Status: store.TaskPending},
		{ID: 2, ChannelID: 1, RequesterID: 200, Prompt: "bob task", Status: store.TaskRunning},
		{ID: 3, ChannelID: 1, RequesterID: 200, Prompt: "bob old", Status: store.TaskDone},
	}}
	s := newTaskTestScheduler(fs)
	alice := turnOrigin{ChannelID: 1, AuthorID: 100}

	out, err := execTaskTool(t, s, alice, `{"command":"list"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"#1 status=pending", "requester=alice", "#2 status=running", "requester=bob", `prompt="bob task"`, "#3 status=done"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}

	var runningCancelled bool
	s.cancels.register(2, func() { runningCancelled = true })

	tests := []struct {
		name    string
		id      string
		wantErr error
	}{
		{name: "someone else's pending", id: "1"},
		{name: "someone else's running", id: "2"},
		{name: "already finished", id: "3", wantErr: store.ErrTaskFinished},
		{name: "unknown", id: "99", wantErr: store.ErrTaskNotFound},
		{name: "missing id", id: "0", wantErr: errTaskIDMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := execTaskTool(t, s, alice, `{"command":"cancel","id":`+tt.id+`}`)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("cancel %s: %v", tt.id, err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("cancel %s: err = %v, want %v", tt.id, err, tt.wantErr)
			}
		})
	}
	if fs.tasks[0].Status != store.TaskCancelled || fs.tasks[1].Status != store.TaskCancelled {
		t.Errorf("statuses after cancel = %s, %s, want cancelled", fs.tasks[0].Status, fs.tasks[1].Status)
	}
	if !runningCancelled {
		t.Error("cancelling a running task did not cancel its turn context")
	}
}

func TestSchedulerRunHandsInterruptedTasksToReport(t *testing.T) {
	fs := &fakeTaskStore{interrupted: []store.Task{
		{ID: 4, ChannelID: 10, RequesterID: 100, Prompt: "research\nthe thing"},
		{ID: 5, ChannelID: 11, RequesterID: 200, Prompt: "check the site"},
	}}
	s := newTaskTestScheduler(fs)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Run(ctx)
	s.Wait()

	notes := interruptedNotes(<-s.interrupted)
	if len(notes) != 2 {
		t.Fatalf("got %d notes, want one per interrupted task", len(notes))
	}
	for i, want := range []struct {
		channel, mention snowflake.ID
		facts            []string
	}{
		{10, 100, []string{"task #4", "<@100>", `"research the thing"`, "will not run again"}},
		{11, 200, []string{"task #5", "<@200>", `"check the site"`}},
	} {
		n := notes[i]
		if n.channelID != want.channel || n.mention != want.mention {
			t.Errorf("note %d channel=%s mention=%s, want %s %s", i, n.channelID, n.mention, want.channel, want.mention)
		}
		for _, f := range want.facts {
			if !strings.Contains(n.trigger, f) {
				t.Errorf("note %d trigger missing %q: %s", i, f, n.trigger)
			}
		}
	}
}

func TestTaskMessageMentionsRequesterOnce(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"adds mention", "trink wos", "<@100> trink wos"},
		{"keeps existing mention", "<@100> trink wos", "<@100> trink wos"},
		{"keeps mention mid text", "hey <@100> trink wos", "hey <@100> trink wos"},
		{"keeps nick mention", "<@!100> trink wos", "<@!100> trink wos"},
		{"other user mention still prefixed", "<@1000> trink wos", "<@100> <@1000> trink wos"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskMessage(snowflake.ID(100), tt.text); got != tt.want {
				t.Errorf("taskMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReactWithoutMessageReturnsError(t *testing.T) {
	a := &Agent{}
	tool, ok := a.buildTools(turnOrigin{ChannelID: 1, AuthorID: 2, TaskID: 3}).Find("discord_react")
	if !ok {
		t.Fatal("discord_react tool not found")
	}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"emojis":["x"]}`))
	if !errors.Is(err, errNoMessageToReact) {
		t.Fatalf("react without message: err = %v, want errNoMessageToReact", err)
	}
}

func TestCapTextKeepsValidUTF8(t *testing.T) {
	got := capText("äää", 3)
	if got != "ä..." {
		t.Errorf("capText() = %q, want %q", got, "ä...")
	}
}
