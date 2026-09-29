package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"os/user"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"github.com/leikonga/doofus-rick/internal/agent"
	"github.com/leikonga/doofus-rick/internal/archive"
	"github.com/leikonga/doofus-rick/internal/config"
	discordpkg "github.com/leikonga/doofus-rick/internal/discord"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/runtimehome"
	"github.com/leikonga/doofus-rick/internal/selbst"
	"github.com/leikonga/doofus-rick/internal/shell"
	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/leikonga/doofus-rick/internal/tracer"
	"github.com/leikonga/doofus-rick/internal/web"
)

const (
	envProduction = "production"
	workGroup     = "rickwork"
)

func main() {
	// File capabilities put the Go runtime in secure mode, which forces GOTRACEBACK=none.
	debug.SetTraceback("single")
	syscall.Umask(0o002)
	stdoutHandler := slog.NewTextHandler(os.Stdout, nil)
	slog.SetDefault(slog.New(stdoutHandler))

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "forget":
			handleForget()
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand %q; valid: forget\n", os.Args[1])
			os.Exit(1)
		}
	}

	if err := run(); err != nil {
		slog.Error("doofus-rick stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	c := config.LoadConfig()

	stdoutHandler := slog.Default().Handler()
	sink := stdoutHandler
	home, homeErr := runtimehome.Open(c.WorkDir, time.Now())
	if homeErr == nil {
		defer home.Close()
		sink = slog.NewMultiHandler(stdoutHandler, home.Handler())
	}
	slog.SetDefault(slog.New(sink))
	if homeErr != nil {
		slog.Warn("runtime home unavailable, logging to stdout only", "work_dir", c.WorkDir, "error", homeErr)
	} else {
		shareWorkDir(c.WorkDir)
		if err := home.RecordBoot(selbst.Commit(), time.Now()); err != nil {
			slog.Warn("failed to record boot in deploy journal", "error", err)
		}
	}

	if os.Getenv("APP_ENV") == envProduction {
		if _, err := shell.Credential(c.ShellUser); err != nil {
			return fmt.Errorf("sys_shell cannot run as its own user %s: %w", c.ShellUser, err)
		}
	}

	db, err := store.Open(ctx, c.DSN())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	if os.Getenv("APP_ENV") != envProduction {
		db.MaybeSeed(ctx)
	}

	tr := tracer.New(func(e *tracer.Entry) {
		tctx, tcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer tcancel()
		blob, err := json.Marshal(e)
		if err != nil {
			slog.Warn("failed to marshal failure trace", "error", err)
			return
		}
		if err := db.SaveFailureTrace(tctx, store.FailureTrace{
			TraceID:   e.ID,
			ChannelID: e.ChannelID,
			UserID:    e.UserID,
			Blob:      string(blob),
			Decline:   e.Decline,
			ErrMsg:    e.Err,
		}); err != nil {
			slog.Warn("failed to save failure trace", "error", err)
		}
	})

	llmClient := llm.NewClient(c.OpenRouterAPIKey)
	retriever := archive.NewRetriever(archive.RetrievalConfig{
		TopK:           c.RecallTopK,
		MinScore:       c.RecallMinScore,
		EmbedModel:     c.RickEmbedModel,
		NeighborChunks: c.RecallNeighborChunks,
	}, db, llmClient)
	aff := archive.NewAffinity(archive.AffinityConfig{Baseline: c.AffinityBaseline}, db)

	rick, err := discordpkg.New(c, db, llmClient, aff)
	if err != nil {
		return fmt.Errorf("create discord client: %w", err)
	}
	ag := agent.New(c, agent.Deps{
		Store:     db,
		LLM:       llmClient,
		Discord:   rick,
		Client:    rick.Client(),
		Retriever: retriever,
		Affinity:  aff,
		Home:      home,
		Tracer:    tr,
	})

	errCh := make(chan error, 2)
	go func() {
		if err := rick.Open(ctx, ag); err != nil {
			errCh <- fmt.Errorf("connect to discord: %w", err)
		}
	}()

	srv := web.NewServer(db, c, rick, tr)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	httpSrv := &http.Server{Addr: c.Port, Handler: mux}
	go func() {
		slog.Info("starting web server", "port", c.Port)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("web server: %w", err)
		}
	}()

	var pprofSrv *http.Server
	if c.PprofAddr != "" {
		pprofSrv = newPprofServer(c.PprofAddr)
		go func() {
			slog.Info("starting pprof server", "addr", c.PprofAddr)
			if err := pprofSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("pprof server failed", "addr", c.PprofAddr, "error", err)
			}
		}()
	}

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-errCh:
		runErr = err
	}
	if err := httpSrv.Shutdown(context.Background()); err != nil {
		slog.Error("failed to shut down web server", "error", err)
	}
	if pprofSrv != nil {
		if err := pprofSrv.Shutdown(context.Background()); err != nil {
			slog.Error("failed to shut down pprof server", "error", err)
		}
	}

	return runErr
}

func newPprofServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return &http.Server{Addr: addr, Handler: mux}
}

func shareWorkDir(workDir string) {
	group, err := user.LookupGroup(workGroup)
	if err != nil {
		slog.Warn("work group missing, skipping work dir perms migration", "group", workGroup, "error", err)
		return
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		slog.Warn("invalid work group id, skipping work dir perms migration", "group", workGroup, "gid", group.Gid, "error", err)
		return
	}
	if err := runtimehome.ShareWithGroup(workDir, gid); err != nil {
		slog.Error("work dir perms migration failed", "work_dir", workDir, "error", err)
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
	db, err := store.Open(context.Background(), c.DSN())
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}

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
