package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

func TestPersonProfileUpsertAndDelete(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()

	if _, err := s.GetPersonProfile(ctx, 7); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := s.SavePersonProfile(ctx, store.PersonProfile{UserID: 7, Summary: "one", WatermarkMessageID: 10}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePersonProfile(ctx, store.PersonProfile{UserID: 7, Summary: "two", WatermarkMessageID: 20, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPersonProfile(ctx, 7)
	if err != nil || got.Summary != "two" || got.WatermarkMessageID != 20 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := s.DeletePersonProfile(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPersonProfile(ctx, 7); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("after delete err = %v, want ErrNotFound", err)
	}
	if err := s.DeletePersonProfile(ctx, 7); err != nil {
		t.Errorf("deleting a missing profile: %v", err)
	}
}

func profileMsg(id, channel, author uint64, content string, bot bool) store.Message {
	return store.Message{ID: id, ChannelID: channel, AuthorID: author, AuthorName: "a", Content: content, IsBot: bot, CreatedAt: time.Now()}
}

func TestGetProfileCandidates(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()

	var msgs []store.Message
	id := uint64(1)
	add := func(n int, channel, author uint64, bot bool) {
		for range n {
			msgs = append(msgs, profileMsg(id, channel, author, "hello there", bot))
			id++
		}
	}
	add(5, 1, 100, false)
	add(3, 1, 200, false)
	add(5, 2, 300, false)
	add(5, 1, 400, true)
	add(5, 1, 500, false)
	add(4, 1, 600, false)
	add(4, 2, 600, false)
	add(5, 1, 700, false)
	msgs = append(msgs, profileMsg(id, 1, 200, "/slash", false), profileMsg(id+1, 1, 200, "ok", false))
	mustCreateMessages(t, s, msgs...)

	if err := s.ForgetAuthor(ctx, 500); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePersonProfile(ctx, store.PersonProfile{UserID: 700, Summary: "x", WatermarkMessageID: 1000}); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetProfileCandidates(ctx, []uint64{1}, 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	var authors []uint64
	for _, c := range got {
		authors = append(authors, c.AuthorID)
		if c.Watermark != 0 {
			t.Errorf("author %d watermark = %d", c.AuthorID, c.Watermark)
		}
	}
	if !slices.Equal(authors, []uint64{100}) {
		t.Errorf("authors = %v, want [100] (200 too few valid messages, 300 other channel, 400 bot, 500 forgotten, 600 only 4 here, 700 watermarked)", authors)
	}

	got, err = s.GetProfileCandidates(ctx, []uint64{1, 2}, 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	authors = nil
	for _, c := range got {
		authors = append(authors, c.AuthorID)
	}
	if !slices.Equal(authors, []uint64{600, 100, 300}) {
		t.Errorf("authors = %v, want most active first [600 100 300]", authors)
	}

	if err := s.SavePersonProfile(ctx, store.PersonProfile{UserID: 100, Summary: "x", WatermarkMessageID: 3}); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetProfileCandidates(ctx, []uint64{1}, 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.AuthorID == 100 {
			t.Errorf("author 100 has only 2 messages above watermark, got candidate %+v", c)
		}
	}

	limited, err := s.GetProfileCandidates(ctx, []uint64{1, 2}, 1, 1)
	if err != nil || len(limited) != 1 {
		t.Errorf("limit: %v, %v", limited, err)
	}
	none, err := s.GetProfileCandidates(ctx, nil, 1, 10)
	if err != nil || len(none) != 0 {
		t.Errorf("no channels: %v, %v", none, err)
	}
}

func TestGetAuthorMessagesAfter(t *testing.T) {
	s := pgtest.Store(t)
	ctx := context.Background()
	mustCreateMessages(t, s,
		profileMsg(1, 1, 7, "first msg", false),
		profileMsg(2, 1, 7, "/command", false),
		profileMsg(3, 1, 7, "hi", false),
		profileMsg(4, 2, 7, "other channel", false),
		profileMsg(5, 1, 8, "someone else", false),
		profileMsg(6, 1, 7, "third msg", false),
		profileMsg(7, 1, 7, "fourth msg", false),
		profileMsg(8, 1, 7, "fifth msg", true),
	)

	got, err := s.GetAuthorMessagesAfter(ctx, 7, 1, []uint64{1}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(messageIDs(got), []uint64{6, 7}) {
		t.Errorf("ids = %v, want [6 7]", messageIDs(got))
	}

	got, err = s.GetAuthorMessagesAfter(ctx, 7, 0, []uint64{1}, 1)
	if err != nil || !slices.Equal(messageIDs(got), []uint64{1}) {
		t.Errorf("limit oldest first: %v, %v", messageIDs(got), err)
	}
}
