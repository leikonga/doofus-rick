package web

import "net/http"

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	quotes := s.store.GetQuotes()
	displayQuotes := make([]QuoteDisplay, len(quotes))

	for i, q := range quotes {
		creator, err := s.bot.GetUsernameForID(q.Creator)
		if err != nil {
			creator = q.Creator
		}
		participants := make([]string, len(q.Participants))
		for j, id := range q.Participants {
			name, err := s.bot.GetUsernameForID(id)
			if err != nil {
				name = id
			}
			participants[j] = name
		}

		displayQuotes[i] = QuoteDisplay{
			Quote:            q,
			CreatorName:      creator,
			ParticipantNames: participants,
		}
	}

	data := map[string]any{"Quotes": displayQuotes}

	if r.Header.Get("HX-Request") != "" {
		s.render(w, "quote_list", data)
		return
	}

	s.render(w, "layout", data)
}
