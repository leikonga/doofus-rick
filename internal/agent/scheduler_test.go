package agent

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/store"
)

type postedMessage struct {
	channelID snowflake.ID
	content   string
}

func TestSchedulerRunTask(t *testing.T) {
	task := store.Task{ID: 3, ChannelID: 10, RequesterID: 100, Prompt: "check the site"}
	tests := []struct {
		name         string
		reply        taskReply
		turnErr      error
		cancelInTurn bool
		postErr      error
		wantPosts    []postedMessage
		wantFinished []finishedTask
	}{
		{
			name:         "reply is posted and recorded done",
			reply:        taskReply{Text: "site is up"},
			wantPosts:    []postedMessage{{10, "<@100> site is up"}},
			wantFinished: []finishedTask{{3, store.TaskDone, "site is up"}},
		},
		{
			name:         "declined reply is not posted",
			reply:        taskReply{Declined: true},
			wantFinished: []finishedTask{{3, store.TaskDone, ""}},
		},
		{
			name:         "turn error fails the task without posting",
			turnErr:      errors.New("llm down"),
			wantFinished: []finishedTask{{3, store.TaskFailed, "llm down"}},
		},
		{
			name:         "post error fails the task",
			reply:        taskReply{Text: "site is up"},
			postErr:      errors.New("discord down"),
			wantPosts:    []postedMessage{{10, "<@100> site is up"}},
			wantFinished: []finishedTask{{3, store.TaskFailed, "discord down"}},
		},
		{
			name:         "cancel during turn records nothing",
			reply:        taskReply{Text: "site is up"},
			cancelInTurn: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := &fakeTaskStore{}
			var posts []postedMessage
			var s *Scheduler
			turn := func(_ context.Context, got store.Task) (taskReply, error) {
				if tc.cancelInTurn {
					s.cancels.cancel(got.ID)
				}
				return tc.reply, tc.turnErr
			}
			post := func(_ context.Context, channelID snowflake.ID, content string) error {
				posts = append(posts, postedMessage{channelID, content})
				return tc.postErr
			}
			s = newScheduler(fs, time.Minute, turn, post, func(snowflake.ID) string { return "" })

			s.runTask(context.Background(), task)

			if !slices.Equal(posts, tc.wantPosts) {
				t.Errorf("posts = %v, want %v", posts, tc.wantPosts)
			}
			if !slices.Equal(fs.finished, tc.wantFinished) {
				t.Errorf("finished = %v, want %v", fs.finished, tc.wantFinished)
			}
		})
	}
}
