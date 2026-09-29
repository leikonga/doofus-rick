package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/sessions"
)

func newAuthTestServer(authEnabled bool) *Server {
	return &Server{
		authEnabled: authEnabled,
		session:     sessions.NewCookieStore([]byte("0123456789abcdef0123456789abcdef")),
	}
}

func markedHandler(called *bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	}
}

func TestAuthMiddlewareDisabledCallsNext(t *testing.T) {
	s := newAuthTestServer(false)
	var called bool
	rec := httptest.NewRecorder()

	s.authMiddleware(markedHandler(&called))(rec, httptest.NewRequest(http.MethodGet, "/debug", nil))

	if !called {
		t.Error("next not called with auth disabled")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuthMiddlewareNoSessionRedirectsToLogin(t *testing.T) {
	s := newAuthTestServer(true)
	var called bool
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/trace/7", nil)

	s.authMiddleware(markedHandler(&called))(rec, req)

	if called {
		t.Error("next called without a session")
	}
	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTemporaryRedirect)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}

	replay := httptest.NewRequest(http.MethodGet, "/login", nil)
	for _, c := range rec.Result().Cookies() {
		replay.AddCookie(c)
	}
	session, err := s.session.Get(replay, SessionKey)
	if err != nil {
		t.Fatalf("decode session cookie: %v", err)
	}
	if got := session.Values["return_url"]; got != "/debug/trace/7" {
		t.Errorf("return_url = %v, want /debug/trace/7", got)
	}
}

func TestAuthMiddlewareAuthenticatedSessionCallsNext(t *testing.T) {
	s := newAuthTestServer(true)

	seed := httptest.NewRecorder()
	session, err := s.session.Get(httptest.NewRequest(http.MethodGet, "/", nil), SessionKey)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	session.Values["authenticated"] = true
	if err := session.Save(httptest.NewRequest(http.MethodGet, "/", nil), seed); err != nil {
		t.Fatalf("save session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/debug", nil)
	for _, c := range seed.Result().Cookies() {
		req.AddCookie(c)
	}
	var called bool
	rec := httptest.NewRecorder()

	s.authMiddleware(markedHandler(&called))(rec, req)

	if !called {
		t.Error("next not called for authenticated session")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuthMiddlewareUnauthenticatedSessionRedirects(t *testing.T) {
	s := newAuthTestServer(true)

	seed := httptest.NewRecorder()
	session, err := s.session.Get(httptest.NewRequest(http.MethodGet, "/", nil), SessionKey)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	session.Values["authenticated"] = false
	if err := session.Save(httptest.NewRequest(http.MethodGet, "/", nil), seed); err != nil {
		t.Fatalf("save session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/debug", nil)
	for _, c := range seed.Result().Cookies() {
		req.AddCookie(c)
	}
	var called bool
	rec := httptest.NewRecorder()

	s.authMiddleware(markedHandler(&called))(rec, req)

	if called {
		t.Error("next called for authenticated=false session")
	}
	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTemporaryRedirect)
	}
}

func TestHandleLoginAuthDisabled(t *testing.T) {
	s := newAuthTestServer(false)
	rec := httptest.NewRecorder()

	s.handleLogin(rec, httptest.NewRequest(http.MethodGet, "/login", nil))

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}
}
