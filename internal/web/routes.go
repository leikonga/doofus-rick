package web

import (
	"net/http"
	"strings"

	"github.com/leikonga/doofus-rick/internal/store"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	quotes := s.store.GetQuotes()
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
	quote, err := s.store.GetQuote(id)
	if err != nil {
		http.NotFound(w, r)
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
	quotes := s.store.GetQuotes()
	displayQuotes := []QuoteDisplay{}

	for _, quote := range quotes {
		creator, err := s.bot.GetUsernameForID(quote.Creator)
		if err != nil {
			creator = quote.Creator
		}

		participants := s.getParticipants(quote)
		display := QuoteDisplay{
			Quote:            quote,
			CreatorName:      creator,
			ParticipantNames: participants,
		}

		if s.matchesQuery(query, display) {
			displayQuotes = append(displayQuotes, display)
		}
	}

	s.render(w, QuoteResults(displayQuotes))
}

func (s *Server) matchesQuery(query string, display QuoteDisplay) bool {
	if query == "" {
		return true
	}

	query = strings.ToLower(query)

	if strings.Contains(strings.ToLower(display.Content), query) {
		return true
	}

	if strings.Contains(strings.ToLower(display.CreatorName), query) {
		return true
	}

	for _, participant := range display.ParticipantNames {
		if strings.Contains(strings.ToLower(participant), query) {
			return true
		}
	}

	return false
}

func (s *Server) getParticipants(q store.Quote) (participants []string) {
	participants = make([]string, len(q.Participants))
	for j, id := range q.Participants {
		name, err := s.bot.GetUsernameForID(id)
		if err != nil {
			name = id
		}
		participants[j] = name
	}
	return
}
