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
	"github.com/leikonga/doofus-rick/internal/logbuf"
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
	store          *store.Store
	config         *config.Config
	llm            *llm.Client
	discord        DiscordState
	discordClient  *disgobot.Client
	brave          *client.BraveClient
	giphy          *client.GiphyClient
	shell          *client.Shell
	logBuf         *logbuf.Buffer
	tracer         *tracer.Tracer
	retriever      *archive.Retriever
	affinity       *archive.Affinity
	typingTheatre  *archive.TypingTheatre
	typingChannels sync.Map // snowflake.ID -> struct{} (channels with active typing indicator)
	codeedit       *codeedit.Editor
	turnTimeout    time.Duration
	repoMu         sync.RWMutex
	selfcode       *selfcode.Selfcode
	cmdRunner      selfcode.Runner
}

func New(s *store.Store, c *config.Config, ds DiscordState, dc *disgobot.Client, lb *logbuf.Buffer, tr *tracer.Tracer) *Agent {
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
	return &Agent{
		store:         s,
		config:        c,
		llm:           llmClient,
		discord:       ds,
		discordClient: dc,
		brave:         client.NewBrave(httpClient, c.BraveAPIKey),
		giphy:         client.NewGiphy(httpClient, c.GiphyAPIKey),
		shell:         client.NewShell(c.WorkDir, shellTimeout),
		logBuf:        lb,
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
		codeedit:    editor,
		turnTimeout: turnTimeout,
		selfcode:    sc,
		cmdRunner:   cmdRunner,
	}
}
