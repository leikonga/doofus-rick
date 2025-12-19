package web

import "github.com/konga-dev/doofus-rick/internal/store"

type QuoteDisplay struct {
	store.Quote

	CreatorName      string
	ParticipantNames []string
}
