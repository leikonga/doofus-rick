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

	"github.com/leikonga/doofus-rick/internal/affinity"
	"github.com/leikonga/doofus-rick/internal/agent"
	"github.com/leikonga/doofus-rick/internal/ambient"
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
	envProduction   = "production"
	shutdownTimeout = 8 * time.Second
	workGroup       = "rickwork"
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
			if err := forget(os.Args[2:]); err != nil {
				slog.Error("forget failed", "error", err)
				os.Exit(1)
			}
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
		defer func() { _ = home.Close() }()
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
		MinSimilarity:  c.RecallMinSimilarity,
		EmbedModel:     c.RickEmbedModel,
		NeighborChunks: c.RecallNeighborChunks,
		RewriteModel:   c.RecallRewriteModel,
	}, db, llmClient)
	aff := affinity.New(affinity.Config{Baseline: c.AffinityBaseline}, db)

	rick, err := discordpkg.New(c, db)
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

	chunker := archive.NewChunker(archive.ChunkConfig{
		ChunkGap:      c.ChunkGap,
		ChunkMaxMsgs:  c.ChunkMaxMsgs,
		ChunkMaxChars: c.ChunkMaxChars,
	}, rick)
	embedder := archive.NewEmbedder(archive.EmbeddingConfig{Model: c.RickEmbedModel}, db, llmClient)
	var scorer archive.ChunkScorer
	if c.AffinityEnabled {
		affinityModel := c.AffinityModel
		if affinityModel == "" {
			affinityModel = c.RickModel
		}
		scorer = affinity.NewScorer(affinity.ScorerConfig{Model: affinityModel}, llmClient, aff, db)
	}
	ingest := archive.NewIngest(archive.IngestConfig{
		ArchiveEnabled:  c.ArchiveEnabled,
		BackfillEnabled: c.BackfillEnabled,
		DenyList:        c.ArchiveDenyChannels,
		GuildID:         c.DiscordGuild,
		BackfillDelay:   c.BackfillDelay,
		BackfillBatch:   c.BackfillBatch,
		ChunkGap:        c.ChunkGap,
		EmbedModel:      c.RickEmbedModel,
	}, db, rick.Client().Rest, rick.Client().ID, chunker, embedder, scorer)

	gate := ambient.NewGate(ambient.GateConfig{
		Enabled:      c.AmbientEnabled,
		Window:       c.AmbientWindow,
		MinMsgs:      c.AmbientMinMsgs,
		MinAuthors:   c.AmbientMinAuthors,
		Cooldown:     c.AmbientCooldown,
		DailyCap:     c.AmbientDailyCap,
		EvalDebounce: c.AmbientEvalDebounce,
		MinScore:     c.AmbientMinScore,
	}, db)
	classifierModel := c.AmbientModel
	if classifierModel == "" {
		classifierModel = c.RickModel
	}
	classifier := ambient.NewClassifier(ambient.ClassifierConfig{
		Model:     classifierModel,
		MaxTokens: c.AmbientMaxTokens,
		MinScore:  c.AmbientMinScore,
	}, llmClient, db)
	watcher := ambient.NewWatcher(ambient.WatcherConfig{Enabled: c.AmbientEnabled, Window: c.AmbientWindow}, db, gate, classifier, ag, rick.Client().ID)

	errCh := make(chan error, 2)
	go func() {
		if err := rick.Open(ctx, discordpkg.Handlers{Agent: ag, Archive: ingest, Ambient: watcher, Tasks: ag.Scheduler()}); err != nil {
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
	cancel()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		slog.Error("failed to shut down web server", "error", err)
	}
	if pprofSrv != nil {
		if err := pprofSrv.Shutdown(shutdownCtx); err != nil {
			slog.Error("failed to shut down pprof server", "error", err)
		}
	}
	rick.Close(shutdownCtx)

	if waitAll(shutdownCtx, rick.Wait, ingest.Wait, watcher.Wait, ag.Scheduler().Wait, ag.Wait) {
		slog.Info("shutdown complete")
	} else {
		slog.Warn("shutdown timed out, abandoning goroutines still running", "timeout", shutdownTimeout)
	}

	return runErr
}

func waitAll(ctx context.Context, waits ...func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, wait := range waits {
			wait()
		}
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
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

type forgetArgs struct {
	messageID uint64
	authorID  uint64
	quotes    bool
}

func parseForgetArgs(args []string) (forgetArgs, error) {
	var (
		fa      forgetArgs
		message string
		author  string
	)
	fs := flag.NewFlagSet("forget", flag.ContinueOnError)
	fs.StringVar(&message, "message", "", "Delete a message by ID")
	fs.StringVar(&author, "author", "", "Delete all messages from an author")
	fs.BoolVar(&fa.quotes, "quotes", false, "Also delete quotes for the author")
	if err := fs.Parse(args); err != nil {
		return forgetArgs{}, err
	}
	if fs.NArg() > 0 {
		return forgetArgs{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if message == "" && author == "" {
		return forgetArgs{}, errors.New("usage: forget --message <id> | --author <snowflake> [--quotes]")
	}
	if fa.quotes && author == "" {
		return forgetArgs{}, errors.New("--quotes requires --author")
	}

	var err error
	if message != "" {
		if fa.messageID, err = strconv.ParseUint(message, 10, 64); err != nil {
			return forgetArgs{}, fmt.Errorf("invalid message id: %w", err)
		}
	}
	if author != "" {
		if fa.authorID, err = strconv.ParseUint(author, 10, 64); err != nil {
			return forgetArgs{}, fmt.Errorf("invalid author id: %w", err)
		}
	}
	return fa, nil
}

func forget(args []string) error {
	fa, err := parseForgetArgs(args)
	if err != nil {
		return err
	}

	c := config.LoadConfig()
	db, err := store.Open(context.Background(), c.DSN())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if fa.messageID != 0 {
		if err := db.DeleteMessage(ctx, fa.messageID); err != nil {
			return fmt.Errorf("delete message %d: %w", fa.messageID, err)
		}
		fmt.Printf("deleted message %d\n", fa.messageID)
	}

	if fa.authorID != 0 {
		if err := db.ForgetAuthor(ctx, fa.authorID); err != nil {
			return fmt.Errorf("forget author %d: %w", fa.authorID, err)
		}
		fmt.Printf("forgotten author %d\n", fa.authorID)

		if fa.quotes {
			if err := db.DeleteQuotesByAuthor(ctx, strconv.FormatUint(fa.authorID, 10)); err != nil {
				return fmt.Errorf("delete quotes for author %d: %w", fa.authorID, err)
			}
			fmt.Printf("deleted quotes for author %d\n", fa.authorID)
		}
	}
	return nil
}
