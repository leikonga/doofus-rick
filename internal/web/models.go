package web

import "github.com/leikonga/doofus-rick/internal/store"

type QuoteDisplay struct {
	store.Quote

	CreatorName      string
	ParticipantNames []string
}
