package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/leikonga/doofus-rick/internal/store"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	quotes, err := s.store.GetQuotes(r.Context())
	if err != nil {
		slog.Warn("failed to get quotes", "error", err)
	}
	displayQuotes := make([]QuoteDisplay, len(quotes))

	for i, quote := range quotes {
		creator, err := s.members.GetUsernameForID(quote.Creator)
		if err != nil {
			creator = quote.Creator
		}

		displayQuotes[i] = QuoteDisplay{
			Quote:            quote,
			CreatorName:      creator,
			ParticipantNames: s.getParticipants(quote),
		}
	}

	if r.Header.Get("HX-Request") != "" {
		s.render(w, QuoteList(displayQuotes))
		return
	}

	s.render(w, QuotesLayout(QuotesPageProps{}, displayQuotes))
}

func (s *Server) handleQuote(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	quote, err := s.store.GetQuote(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			slog.Error("failed to get quote", "id", id, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	creator, err := s.members.GetUsernameForID(quote.Creator)
	if err != nil {
		creator = quote.Creator
	}

	display := QuoteDisplay{
		Quote:            quote,
		CreatorName:      creator,
		ParticipantNames: s.getParticipants(quote),
	}

	s.render(w, QuoteSingleLayout(QuotesPageProps{}, display))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	quotes, err := s.store.SearchQuotes(r.Context(), query)
	if err != nil {
		slog.Warn("failed to search quotes", "query", query, "error", err)
	}
	var displayQuotes []QuoteDisplay

	for _, quote := range quotes {
		creator, err := s.members.GetUsernameForID(quote.Creator)
		if err != nil {
			creator = quote.Creator
		}
		displayQuotes = append(displayQuotes, QuoteDisplay{
			Quote:            quote,
			CreatorName:      creator,
			ParticipantNames: s.getParticipants(quote),
		})
	}

	s.render(w, QuoteResults(displayQuotes))
}

func (s *Server) getParticipants(q store.Quote) (participants []string) {
	if q.Participants == nil {
		return
	}
	participants = make([]string, len(*q.Participants))
	for j, id := range *q.Participants {
		name, err := s.members.GetUsernameForID(id)
		if err != nil {
			name = id
		}
		participants[j] = name
	}
	return
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	name, err := s.members.GetUsernameForID(id)
	if err != nil {
		name = id
	}
	quotes, err := s.store.GetQuotesByUser(r.Context(), id)
	if err != nil {
		slog.Warn("failed to get quotes for user", "user", id, "error", err)
	}
	display := make([]QuoteDisplay, len(quotes))
	for i, quote := range quotes {
		creator, err := s.members.GetUsernameForID(quote.Creator)
		if err != nil {
			creator = quote.Creator
		}
		display[i] = QuoteDisplay{Quote: quote, CreatorName: creator, ParticipantNames: s.getParticipants(quote)}
	}
	s.render(w, UserLayout(QuotesPageProps{}, name, display))
}
