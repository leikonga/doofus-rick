package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/leikonga/doofus-rick/internal/pgtest"
	"github.com/leikonga/doofus-rick/internal/store"
)

type fakeMembers struct {
	names    map[string]string
	guild    map[string]bool
	nameErr  error
	guildErr error
}

func (f fakeMembers) GetUsernameForID(id string) (string, error) {
	if f.nameErr != nil {
		return "", f.nameErr
	}
	name, ok := f.names[id]
	if !ok {
		return "", errors.New("unknown user")
	}
	return name, nil
}

func (f fakeMembers) IsGuildMember(id string) (bool, error) {
	if f.guildErr != nil {
		return false, f.guildErr
	}
	return f.guild[id], nil
}

func TestGetParticipants(t *testing.T) {
	srv := &Server{members: fakeMembers{names: map[string]string{"1": "alice", "2": "bob"}}}

	tests := []struct {
		name         string
		participants *store.StringSlice
		want         []string
	}{
		{"nil", nil, nil},
		{"known", &store.StringSlice{"1", "2"}, []string{"alice", "bob"}},
		{"unknown falls back to id", &store.StringSlice{"1", "3"}, []string{"alice", "3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := srv.getParticipants(store.Quote{Participants: tt.participants})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandleQuoteShowsCreatorName(t *testing.T) {
	s := pgtest.Store(t)
	if err := s.CreateQuote(t.Context(), store.Quote{Content: "hello world", Creator: "42"}); err != nil {
		t.Fatalf("create quote: %v", err)
	}
	srv := &Server{store: s, members: fakeMembers{names: map[string]string{"42": "Zaphod"}}}

	req := httptest.NewRequest(http.MethodGet, "/quote/1", nil)
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	srv.handleQuote(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Zaphod") {
		t.Errorf("body missing creator name")
	}
}
