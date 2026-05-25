package agent

import (
	"net/http"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/client"
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/store"
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
	anthropic      anthropic.Client
	discord        DiscordState
	discordClient  *disgobot.Client
	brave          *client.BraveClient
	giphy          *client.GiphyClient
	shell          *client.Shell
	typingChannels sync.Map // snowflake.ID -> struct{} (channels with active typing indicator)
}

func New(s *store.Store, c *config.Config, ds DiscordState, dc *disgobot.Client) *Agent {
	httpClient := &http.Client{Timeout: 15 * time.Second}
	return &Agent{
		store:         s,
		config:        c,
		anthropic:     anthropic.NewClient(option.WithAPIKey(c.AnthropicAPIKey)),
		discord:       ds,
		discordClient: dc,
		brave:         client.NewBrave(httpClient, c.BraveAPIKey),
		giphy:         client.NewGiphy(httpClient, c.GiphyAPIKey),
		shell:         client.NewShell(c.WorkDir),
	}
}
