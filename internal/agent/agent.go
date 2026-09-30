package agent

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/affinity"
	"github.com/leikonga/doofus-rick/internal/archive"
	"github.com/leikonga/doofus-rick/internal/brave"
	"github.com/leikonga/doofus-rick/internal/codeedit"
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/giphy"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/runtimehome"
	"github.com/leikonga/doofus-rick/internal/sandbox"
	"github.com/leikonga/doofus-rick/internal/selbst"
	"github.com/leikonga/doofus-rick/internal/selfcode"
	"github.com/leikonga/doofus-rick/internal/shell"
	"github.com/leikonga/doofus-rick/internal/store"
	"github.com/leikonga/doofus-rick/internal/tracer"
)

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
	web              webTools
	sys              shellTools
	github           githubTools
	code             codeTools
	selbstBlock      string
	runtimeLogs      runtimeLogs
	deploys          *runtimehome.Journal
	crashFile        string
	tracer           *tracer.Tracer
	retriever        *archive.Retriever
	affinity         *affinity.Ledger
	typist           *typist
	turnTimeout      time.Duration
	tasks            taskStore
	taskWake         chan struct{}
	taskCancels      taskCancels
	interruptedTasks chan []store.Task
	wg               sync.WaitGroup
}

type Deps struct {
	Store     *store.Store
	LLM       *llm.Client
	Discord   DiscordState
	Client    *disgobot.Client
	Retriever *archive.Retriever
	Affinity  *affinity.Ledger
	Home      *runtimehome.Home
	Tracer    *tracer.Tracer
}

func New(c *config.Config, d Deps) *Agent {
	httpClient := &http.Client{Timeout: 15 * time.Second}
	home := d.Home
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
		store:         d.Store,
		config:        c,
		llm:           d.LLM,
		discord:       d.Discord,
		discordClient: d.Client,
		web: webTools{
			brave: brave.New(httpClient, c.BraveAPIKey),
			giphy: giphy.New(httpClient, c.GiphyAPIKey),
		},
		sys: shellTools{
			runner:      shell.New(c.WorkDir, c.ShellTimeout, c.ShellUser),
			desc:        shellDescription(c.ShellUser, c.WorkDir, c.PprofAddr, sandbox.Available(tools)),
			runtimeLogs: logs,
		},
		github: githubTools{config: c},
		code: codeTools{
			editor:   editor,
			selfcode: sc,
			runner:   cmdRunner,
			config:   c,
			deploys:  deploys,
		},
		selbstBlock: self.Block(),
		tracer:      d.Tracer,
		retriever:   d.Retriever,
		affinity:    d.Affinity,
		typist: newTypist(typingTheatreConfig{
			Enabled:  c.TypingTheatre,
			MaxDelay: c.TypingMaxDelay,
			Chance:   c.TypingChance,
		}),
		turnTimeout:      c.RickTurnTimeout,
		tasks:            d.Store,
		taskWake:         make(chan struct{}, 1),
		interruptedTasks: make(chan []store.Task, 1),
	}
}

func (a *Agent) Wait() {
	a.wg.Wait()
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
