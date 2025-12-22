package web

import (
	"encoding/json"
	"net/http"

	"golang.org/x/oauth2"
)

const SessionKey = "doofus-rick-session"

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := s.session.Get(r, SessionKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	url := s.oauthConfig.AuthCodeURL("state", oauth2.AccessTypeOffline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	token, err := s.oauthConfig.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// get currently logged-in user snowflake using the web api
	// then use bot api to check if they're a member of the configured guild
	client := s.oauthConfig.Client(r.Context(), token)
	resp, _ := client.Get("https://discord.com/api/users/@me")
	var user struct {
		ID string `json:"id"`
	}
	err = json.NewDecoder(resp.Body).Decode(&user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if ok, err := s.bot.IsGuildMember(user.ID); err != nil || !ok {
		http.Error(w, "You do not have access to this website", http.StatusForbidden)
		return
	}

	// membership verified, set session data from discord into signed cookie
	session, err := s.session.Get(r, SessionKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	session.Values["authenticated"] = true
	session.Values["token"] = token
	if err := session.Save(r, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
