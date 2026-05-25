package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/leikonga/doofus-rick/internal/config"
	discordpkg "github.com/leikonga/doofus-rick/internal/discord"
	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/leikonga/doofus-rick/internal/web"
)

func main() {
	handler := slog.NewTextHandler(os.Stdout, nil)
	slog.SetDefault(slog.New(handler))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	c := config.LoadConfig()
	db := store.MustInit(c)
	rick := discordpkg.New(ctx, db, c)
	go func() {
		if err := rick.Run(); err != nil {
			slog.Error("failed to connect to discord", "error", err)
			os.Exit(1)
		}
	}()

	srv := web.NewServer(db, c, rick)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	httpSrv := &http.Server{Addr: c.Port, Handler: mux}
	go func() {
		slog.Info("starting web server", "port", c.Port)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("failed to start web server", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	if err := httpSrv.Shutdown(context.Background()); err != nil {
		slog.Error("failed to shut down web server", "error", err)
	}
}
