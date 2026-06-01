package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/leikonga/doofus-rick/internal/config"
	discordpkg "github.com/leikonga/doofus-rick/internal/discord"
	"github.com/leikonga/doofus-rick/internal/logbuf"
	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/leikonga/doofus-rick/internal/tracer"
	"github.com/leikonga/doofus-rick/internal/web"
)

const envProduction = "production"

func main() {
	textHandler := slog.NewTextHandler(os.Stdout, nil)
	logHandler, logBuf := logbuf.New(textHandler)
	slog.SetDefault(slog.New(logHandler))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	c := config.LoadConfig()
	db := store.MustInit(c)
	if os.Getenv("APP_ENV") != envProduction {
		db.MaybeSeed(ctx)
	}

	tr := tracer.New(func(e *tracer.Entry) {
		tctx, tcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer tcancel()
		db.SaveFailureTrace(tctx, e)
	})

	rick := discordpkg.New(ctx, db, c, logBuf, tr)
	go func() {
		if err := rick.Run(); err != nil {
			slog.Error("failed to connect to discord", "error", err)
			os.Exit(1)
		}
	}()

	srv := web.NewServer(db, c, rick, tr)
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
