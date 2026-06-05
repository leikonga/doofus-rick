package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/leikonga/doofus-rick/internal/store"
	"gorm.io/gorm"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	quotes := s.store.GetQuotes(r.Context())
	displayQuotes := make([]QuoteDisplay, len(quotes))

	for i, quote := range quotes {
		creator, err := s.bot.GetUsernameForID(quote.Creator)
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
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
		} else {
			slog.Error("failed to get quote", "id", id, "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	creator, err := s.bot.GetUsernameForID(quote.Creator)
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
	quotes := s.store.SearchQuotes(r.Context(), query)
	var displayQuotes []QuoteDisplay

	for _, quote := range quotes {
		creator, err := s.bot.GetUsernameForID(quote.Creator)
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
		name, err := s.bot.GetUsernameForID(id)
		if err != nil {
			name = id
		}
		participants[j] = name
	}
	return
}
