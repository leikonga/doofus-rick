package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "forget":
			handleForget()
			return
		}
	}

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

func handleForget() {
	var (
		flagMessage string
		flagAuthor  string
		flagQuotes  bool
	)

	flag.StringVar(&flagMessage, "message", "", "Delete a message by ID")
	flag.StringVar(&flagAuthor, "author", "", "Delete all messages from an author")
	flag.BoolVar(&flagQuotes, "quotes", false, "Also delete quotes for the author")
	flag.Parse()

	if flagMessage == "" && flagAuthor == "" {
		fmt.Println("Usage: forget --message <id> | --author <snowflake>")
		os.Exit(1)
	}

	c := config.LoadConfig()
	db := store.MustInit(c)

	if flagMessage != "" {
		id, err := strconv.ParseUint(flagMessage, 10, 64)
		if err != nil {
			slog.Error("invalid message id", "error", err)
			os.Exit(1)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := db.DeleteMessage(ctx, id); err != nil {
			slog.Error("failed to delete message", "error", err)
			os.Exit(1)
		}
		fmt.Printf("deleted message %d\n", id)
	}

	if flagAuthor != "" {
		authorID, err := strconv.ParseUint(flagAuthor, 10, 64)
		if err != nil {
			slog.Error("invalid author id", "error", err)
			os.Exit(1)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := db.ForgetAuthor(ctx, authorID); err != nil {
			slog.Error("failed to forget author", "error", err)
			os.Exit(1)
		}
		fmt.Printf("forgotten author %d\n", authorID)

		if flagQuotes {
			if err := db.DeleteQuotesByAuthor(ctx, flagAuthor); err != nil {
				slog.Error("failed to delete quotes", "error", err)
				os.Exit(1)
			}
			fmt.Printf("deleted quotes for author %d\n", authorID)
		}
	}
}
