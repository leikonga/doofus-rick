package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/leikonga/doofus-rick/internal/pgtest"
)

func TestHandleQuoteMissingReturns404(t *testing.T) {
	srv := &Server{store: pgtest.Store(t), authEnabled: false}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /quote/{id}", srv.authMiddleware(srv.handleQuote))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/quote/999", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
