package agent

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/archive"
	"github.com/leikonga/doofus-rick/internal/client"
	"github.com/leikonga/doofus-rick/internal/codeedit"
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/runtimehome"
	"github.com/leikonga/doofus-rick/internal/sandbox"
	"github.com/leikonga/doofus-rick/internal/selbst"
	"github.com/leikonga/doofus-rick/internal/selfcode"
	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/leikonga/doofus-rick/internal/tracer"
)

// DiscordState is the subset of discord.Bot state the agent reads.
type DiscordState interface {
	GetMemberForID(id string) (*discord.Member, error)
	GetUsernameForID(id string) (string, error)
	OnlineMembers() []discord.Member
	AllMembers() ([]discord.Member, error)
	VoiceChannels() map[snowflake.ID]string
	VoiceChannelForID(id string) string
	GetStatusForID(id string) discord.OnlineStatus
	GetActivitiesForID(id string) []discord.Activity
}

type Agent struct {
	store            *store.Store
	config           *config.Config
	llm              *llm.Client
	discord          DiscordState
	discordClient    *disgobot.Client
	brave            *client.BraveClient
	giphy            *client.GiphyClient
	shell            *client.Shell
	shellDesc        string
	selbstBlock      string
	runtimeLogs      runtimeLogs
	deploys          *runtimehome.Journal
	crashFile        string
	tracer           *tracer.Tracer
	retriever        *archive.Retriever
	affinity         *archive.Affinity
	typingTheatre    *archive.TypingTheatre
	typingChannels   sync.Map // snowflake.ID -> struct{} (channels with active typing indicator)
	codeedit         *codeedit.Editor
	turnTimeout      time.Duration
	repoMu           sync.RWMutex
	selfcode         *selfcode.Selfcode
	cmdRunner        selfcode.Runner
	tasks            taskStore
	taskWake         chan struct{}
	taskCancels      taskCancels
	interruptedTasks chan []store.Task
}

func New(s *store.Store, c *config.Config, ds DiscordState, dc *disgobot.Client, home *runtimehome.Home, tr *tracer.Tracer) *Agent {
	httpClient := &http.Client{Timeout: 15 * time.Second}
	llmClient := llm.NewClient(c.OpenRouterAPIKey)
	typingMaxDelay, err := time.ParseDuration(c.TypingMaxDelay)
	if err != nil {
		typingMaxDelay = 20 * time.Second
	}
	turnTimeout, err := time.ParseDuration(c.RickTurnTimeout)
	if err != nil {
		turnTimeout = 10 * time.Minute
	}
	shellTimeout, err := time.ParseDuration(c.ShellTimeout)
	if err != nil {
		shellTimeout = 120 * time.Second
	}
	editor, err := codeedit.New(c.RickRepoDir)
	if err != nil {
		slog.Warn("code repo dir not available, code_read and code_edit will error until cloned", "dir", c.RickRepoDir, "error", err)
	}
	cmdRunner := selfcode.ExecRunner{}
	sc := selfcode.New(cmdRunner, c.RickRepoDir, c.BackupsDir, selfcode.DBConfig{
		Host: c.DBHost,
		Port: c.DBPort,
		User: c.DBUser,
		Pass: c.DBPass,
		Name: c.DBName,
	})
	tools, err := sandbox.Manifest()
	if err != nil {
		slog.Error("failed to parse sandbox tool manifest, sys_shell lists no tools", "error", err)
	}
	paths := selbst.Paths{Work: c.WorkDir, Source: c.RickRepoDir}
	var logs runtimeLogs
	var deploys *runtimehome.Journal
	var crashFile string
	if home != nil { // a typed-nil *Home in the interface would defeat logReport's nil check
		logs = home
		deploys = home.Deploys()
		crashFile = home.CrashFile()
		paths.Logs, paths.Crash, paths.Deploys = home.LogsDir(), home.CrashDir(), home.DeploysPath()
	}
	self := selbst.Gather(c.RickModel, c.ShellUser, c.PprofAddr, paths)
	return &Agent{
		runtimeLogs:   logs,
		deploys:       deploys,
		crashFile:     crashFile,
		store:         s,
		config:        c,
		llm:           llmClient,
		discord:       ds,
		discordClient: dc,
		brave:         client.NewBrave(httpClient, c.BraveAPIKey),
		giphy:         client.NewGiphy(httpClient, c.GiphyAPIKey),
		shell:         client.NewShell(c.WorkDir, shellTimeout, c.ShellUser),
		shellDesc:     shellDescription(c.ShellUser, c.WorkDir, c.PprofAddr, sandbox.Available(tools)),
		selbstBlock:   self.Block(),
		tracer:        tr,
		retriever: archive.NewRetriever(archive.RetrievalConfig{
			TopK:           c.RecallTopK,
			MinScore:       c.RecallMinScore,
			EmbedModel:     c.RickEmbedModel,
			NeighborChunks: c.RecallNeighborChunks,
		}, s, llmClient),
		affinity: archive.NewAffinity(archive.AffinityConfig{
			Baseline:    c.AffinityBaseline,
			DecayPerDay: c.AffinityDecayPerDay,
			Model:       c.AffinityModel,
		}, s),
		typingTheatre: archive.NewTypingTheatre(archive.TypingTheatreConfig{
			Enabled:  c.TypingTheatre,
			MaxDelay: typingMaxDelay,
			Chance:   c.TypingChance,
		}),
		codeedit:         editor,
		turnTimeout:      turnTimeout,
		selfcode:         sc,
		cmdRunner:        cmdRunner,
		tasks:            s,
		taskWake:         make(chan struct{}, 1),
		interruptedTasks: make(chan []store.Task, 1),
	}
}

func (a *Agent) vitals(now time.Time) string {
	return selbst.Vitals(now, a.deployStatus(now))
}

func (a *Agent) deployStatus(now time.Time) string {
	if a.deploys == nil {
		return "unknown"
	}
	records, err := a.deploys.Records()
	if err != nil {
		slog.Warn("failed to read deploy journal", "error", err)
		return "unknown"
	}
	return selbst.DeployStatus(records, selbst.Commit(), now)
}
