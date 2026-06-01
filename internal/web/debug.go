package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"

	"github.com/leikonga/doofus-rick/internal/tracer"
)

func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
	entries := s.mergeTraces(r)
	s.render(w, DebugListPage(entries))
}

func (s *Server) handleDebugTrace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if e := s.tracer.FindByID(id); e != nil {
		s.render(w, DebugTracePage(e))
		return
	}

	ft, err := s.store.GetFailureTraceByTraceID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var e tracer.Entry
	if err := json.Unmarshal([]byte(ft.Blob), &e); err != nil {
		slog.Warn("failed to decode failure trace blob", "id", id, "error", err)
		http.Error(w, "failed to decode trace", http.StatusInternalServerError)
		return
	}
	s.render(w, DebugTracePage(&e))
}

func (s *Server) mergeTraces(r *http.Request) []*tracer.Entry {
	successes := s.tracer.RecentSuccesses()

	failures, err := s.store.GetFailureTraces(r.Context(), 200)
	if err != nil {
		slog.Warn("failed to get failure traces", "error", err)
	}

	all := make([]*tracer.Entry, 0, len(successes)+len(failures))
	all = append(all, successes...)
	for _, ft := range failures {
		var e tracer.Entry
		if err := json.Unmarshal([]byte(ft.Blob), &e); err != nil {
			slog.Warn("failed to decode failure trace", "id", ft.TraceID, "error", err)
			continue
		}
		all = append(all, &e)
	}

	slices.SortFunc(all, func(a, b *tracer.Entry) int {
		return b.At.Compare(a.At)
	})
	return all
}
