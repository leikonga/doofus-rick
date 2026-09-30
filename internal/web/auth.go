package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"golang.org/x/oauth2"
)

const SessionKey = "doofus-rick-session"

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authEnabled {
			next(w, r)
			return
		}
		session, err := s.session.Get(r, SessionKey)
		if err != nil {
			httpFail(w, http.StatusInternalServerError, "load session", err)
			return
		}
		if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
			session.Values["return_url"] = r.URL.Path
			if err := session.Save(r, w); err != nil {
				httpFail(w, http.StatusInternalServerError, "save session", err)
				return
			}
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled {
		http.Error(w, "auth not configured", http.StatusNotImplemented)
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		httpFail(w, http.StatusInternalServerError, "generate oauth state", err)
		return
	}
	state := base64.URLEncoding.EncodeToString(b)

	session, err := s.session.Get(r, SessionKey)
	if err != nil {
		httpFail(w, http.StatusInternalServerError, "load session", err)
		return
	}
	session.Values["oauth_state"] = state
	if err := session.Save(r, w); err != nil {
		httpFail(w, http.StatusInternalServerError, "save session", err)
		return
	}

	url := s.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	session, err := s.session.Get(r, SessionKey)
	if err != nil {
		httpFail(w, http.StatusInternalServerError, "load session", err)
		return
	}
	savedState, ok := session.Values["oauth_state"].(string)
	if !ok || savedState == "" || savedState != r.FormValue("state") {
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}
	delete(session.Values, "oauth_state")

	token, err := s.oauthConfig.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		httpFail(w, http.StatusBadRequest, "exchange oauth code", err)
		return
	}

	client := s.oauthConfig.Client(r.Context(), token)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://discord.com/api/users/@me", nil)
	if err != nil {
		httpFail(w, http.StatusInternalServerError, "build discord user request", err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		httpFail(w, http.StatusBadGateway, "fetch discord user", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		httpFail(w, http.StatusBadGateway, "fetch discord user", fmt.Errorf("status %s", resp.Status))
		return
	}
	var user struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		httpFail(w, http.StatusBadGateway, "decode discord user", err)
		return
	}
	ok, err = s.members.IsGuildMember(user.ID)
	if err != nil {
		httpFail(w, http.StatusBadGateway, "check guild membership", err)
		return
	}
	if !ok {
		http.Error(w, "You do not have access to this website", http.StatusForbidden)
		return
	}

	returnURL := "/"
	if url, ok := session.Values["return_url"].(string); ok && url != "" {
		returnURL = url
		delete(session.Values, "return_url")
	}

	session.Values["authenticated"] = true
	session.Values["token"] = token
	if err := session.Save(r, w); err != nil {
		httpFail(w, http.StatusInternalServerError, "save session", err)
		return
	}

	http.Redirect(w, r, returnURL, http.StatusSeeOther)
}

func httpFail(w http.ResponseWriter, status int, op string, err error) {
	level := slog.LevelWarn
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	slog.Log(context.Background(), level, "web request failed", "op", op, "status", status, "error", err)
	http.Error(w, http.StatusText(status), status)
}
