package web

import (
	"embed"
	"encoding/gob"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/pprof"

	"github.com/gorilla/sessions"
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/leikonga/doofus-rick/internal/tracer"
	"golang.org/x/oauth2"
	g "maragu.dev/gomponents"
)

//go:embed static/*
var staticFS embed.FS

type members interface {
	GetUsernameForID(id string) (string, error)
	IsGuildMember(id string) (bool, error)
}

type Server struct {
	store       *store.Store
	members     members
	config      *config.Config
	tracer      *tracer.Tracer
	session     *sessions.CookieStore
	oauthConfig *oauth2.Config
	authEnabled bool
}

func NewServer(s *store.Store, c *config.Config, m members, tr *tracer.Tracer) *Server {
	if c.SessionSecret == "" {
		slog.Warn("session secret is not set, sessions will not be persisted")
	}
	if c.DiscordClientID == "" || c.DiscordClientSecret == "" || c.DiscordRedirectURI == "" {
		slog.Warn("discord oauth credentials are not set, web login is disabled")
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

	authEnabled := c.DiscordClientID != "" && c.DiscordClientSecret != "" && c.SessionSecret != ""

	return &Server{
		store:       s,
		members:     m,
		config:      c,
		tracer:      tr,
		session:     sessions.NewCookieStore([]byte(c.SessionSecret)),
		oauthConfig: oa,
		authEnabled: authEnabled,
	}
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	staticRoot, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(staticRoot))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/callback", s.handleCallback)
	mux.HandleFunc("GET /{$}", s.authMiddleware(s.handleHome))
	mux.HandleFunc("GET /search", s.authMiddleware(s.handleSearch))
	mux.HandleFunc("GET /quote/{id}", s.authMiddleware(s.handleQuote))
	mux.HandleFunc("GET /user/{id}", s.authMiddleware(s.handleUser))
	mux.HandleFunc("GET /debug", s.authMiddleware(s.handleDebug))
	mux.HandleFunc("GET /debug/trace/{id}", s.authMiddleware(s.handleDebugTrace))
	mux.HandleFunc("GET /debug/pprof/goroutineleak", s.authMiddleware(pprof.Handler("goroutineleak").ServeHTTP))
}

func (s *Server) render(w http.ResponseWriter, node g.Node) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := node.Render(w); err != nil {
		slog.Error("component render failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
