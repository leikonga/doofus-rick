package main

import (
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/leikonga/doofus-rick/internal/bot"
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/leikonga/doofus-rick/internal/web"
)

func main() {
	handler := slog.NewTextHandler(os.Stdout, nil)
	slog.SetDefault(slog.New(handler))

	c := config.LoadConfig()
	db := store.MustInit(c)
	discord := bot.New(db, c)
	go func() {
		if err := discord.Run(); err != nil {
			slog.Error("failed to connect to discord", "error", err)
			os.Exit(1)
		}
	}()

	srv := web.NewServer(db, c, discord)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	slog.Info("starting web server", "port", c.Port)
	go func() {
		err := http.ListenAndServe(c.Port, mux)
		if err != nil {
			slog.Error("failed to start web server", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}
