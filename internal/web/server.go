package web

import (
	"bytes"
	"embed"
	"encoding/gob"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/leikonga/doofus-rick/internal/bot"
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/store"
	"golang.org/x/oauth2"
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

type Server struct {
	store       *store.Store
	bot         *bot.Bot
	config      *config.Config
	templates   *template.Template
	session     *sessions.CookieStore
	oauthConfig *oauth2.Config
}

func NewServer(s *store.Store, c *config.Config, b *bot.Bot) *Server {
	if c.SessionSecret == "" {
		slog.Warn("session secret is not set, sessions will not be persisted")
	}
	if c.DiscordClientID == "" || c.DiscordClientSecret == "" || c.DiscordRedirectURI == "" {
		slog.Warn("discord oauth credentials are not set, login will not work")
	}

	gob.Register(&oauth2.Token{})
	oa := &oauth2.Config{
		ClientID:     c.DiscordClientID,
		ClientSecret: c.DiscordClientSecret,
		RedirectURL:  c.DiscordRedirectURI,
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://discord.com/api/oauth2/authorize",
			TokenURL: "https://discord.com/api/oauth2/token",
		},
		Scopes: []string{"identify", "guilds"},
	}

	return &Server{
		store:       s,
		bot:         b,
		templates:   template.Must(template.ParseFS(templateFS, "templates/*.gohtml")),
		session:     sessions.NewCookieStore([]byte(c.SessionSecret)),
		oauthConfig: oa,
	}
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	staticRoot, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(staticRoot))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/callback", s.handleCallback)
	mux.HandleFunc("GET /{$}", s.authMiddleware(s.handleHome))
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	buf := new(bytes.Buffer)
	if err := s.templates.ExecuteTemplate(buf, name, data); err != nil {
		slog.Error("template execution failed", "error", err, "template", name, "data", data)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err := buf.WriteTo(w)
	if err != nil {
		return
	}
}
