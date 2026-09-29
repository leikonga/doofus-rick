package discord

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/disgoorg/disgo"
	disgobot "github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/leikonga/doofus-rick/internal/ambient"
	"github.com/leikonga/doofus-rick/internal/config"
	"github.com/leikonga/doofus-rick/internal/llm"
	"github.com/leikonga/doofus-rick/internal/store"
)

type Agent interface {
	HandleMention(ctx context.Context, e *events.MessageCreate)
	HandleAmbient(ctx context.Context, channelID snowflake.ID, hook string) (snowflake.ID, error)
	ReportDeploy(ctx context.Context)
	ReportInterruptedTasks(ctx context.Context)
	RunTasks(ctx context.Context)
}

type Handlers struct {
	Agent   Agent
	Archive Archive
}

type Archive interface {
	RecordLive(ctx context.Context, msg discord.Message, channelID snowflake.ID) bool
	Run(ctx context.Context)
}

// bot.go is a 714 zeilen langer monolith weil oser zu foul woar mia zeit
// zum refactorn zu gebn. wenn du des liest, oser, du toagoff: geh sölm mocha.
type Bot struct {
	store             *store.Store
	config            *config.Config
	llm               *llm.Client
	client            *disgobot.Client
	agent             Agent
	cache             UserCache
	presences         sync.Map // snowflake.ID -> UserPresence
	voiceChannels     sync.Map // snowflake.ID -> string (channel name, empty if unknown)
	ambientGate       *ambient.Gate
	ambientClassifier *ambient.Classifier
	deployReportOnce  sync.Once
	taskReportOnce    sync.Once
}

func New(c *config.Config, s *store.Store, llmClient *llm.Client) (*Bot, error) {
	client, err := disgo.New(c.DiscordToken,
		disgobot.WithGatewayConfigOpts(
			gateway.WithIntents(gateway.IntentGuilds, gateway.IntentGuildMembers, gateway.IntentGuildMessages, gateway.IntentMessageContent, gateway.IntentGuildPresences, gateway.IntentGuildVoiceStates),
		),
	)
	if err != nil {
		return nil, err
	}
	return &Bot{store: s, config: c, client: client, llm: llmClient}, nil
}

func (b *Bot) Client() *disgobot.Client {
	return b.client
}

func (b *Bot) Open(ctx context.Context, h Handlers) error {
	b.agent = h.Agent

	r := handler.New()
	r.SlashCommand("/ping", b.handlePingCommand)
	r.SlashCommand("/quote", b.handleQuote)
	r.SlashCommand("/randomquote", func(d discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		return b.handleRandomQuote(ctx, d, e)
	})
	r.SlashCommand("/mama", b.handleMama)
	r.Modal("/quote", func(e *handler.ModalEvent) error {
		return b.handleQuoteSubmission(ctx, e)
	})

	b.client.AddEventListeners(
		r,
		disgobot.NewListenerFunc(func(e *events.MessageCreate) { h.Agent.HandleMention(ctx, e) }),
		disgobot.NewListenerFunc(b.onGuildReady),
		disgobot.NewListenerFunc(func(*events.GuildReady) { b.reportDeployOnce(ctx) }),
		disgobot.NewListenerFunc(func(*events.GuildReady) { b.reportInterruptedTasksOnce(ctx) }),
		disgobot.NewListenerFunc(b.onPresenceUpdate),
		disgobot.NewListenerFunc(b.onGuildVoiceStateUpdate),
		disgobot.NewListenerFunc(func(e *events.MessageCreate) {
			if !b.config.ArchiveEnabled {
				return
			}
			if h.Archive.RecordLive(ctx, e.Message, e.ChannelID) {
				b.checkAmbient(e.ChannelID)
			}
		}),
	)

	if b.config.AmbientEnabled {
		b.ambientGate = ambient.NewGate(ambient.GateConfig{
			Enabled:      b.config.AmbientEnabled,
			Window:       b.config.AmbientWindow,
			MinMsgs:      b.config.AmbientMinMsgs,
			MinAuthors:   b.config.AmbientMinAuthors,
			Cooldown:     b.config.AmbientCooldown,
			DailyCap:     b.config.AmbientDailyCap,
			EvalDebounce: b.config.AmbientEvalDebounce,
			MinScore:     b.config.AmbientMinScore,
		}, b.store)
		classifierModel := b.config.AmbientModel
		if classifierModel == "" {
			classifierModel = b.config.RickModel
		}
		b.ambientClassifier = ambient.NewClassifier(ambient.ClassifierConfig{
			Model:     classifierModel,
			MaxTokens: b.config.AmbientMaxTokens,
			MinScore:  b.config.AmbientMinScore,
		}, b.llm, b.store)
	}

	if b.config.DiscordGuild == "" {
		slog.Warn("no discord guild configured, skipping command registration")
	} else {
		guildID := snowflake.MustParse(b.config.DiscordGuild)
		if err := handler.SyncCommands(b.client, commands, []snowflake.ID{guildID}); err != nil {
			slog.Error("failed to sync commands", "error", err)
		}
	}

	if err := b.client.OpenGateway(ctx); err != nil {
		return err
	}

	h.Archive.Run(ctx)

	go h.Agent.RunTasks(ctx)

	slog.Info("connected to discord", "appid", b.client.ApplicationID)
	return nil
}

func (b *Bot) reportDeployOnce(ctx context.Context) {
	b.deployReportOnce.Do(func() {
		go func() {
			ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			b.agent.ReportDeploy(ctx)
		}()
	})
}

func (b *Bot) reportInterruptedTasksOnce(ctx context.Context) {
	b.taskReportOnce.Do(func() {
		go func() {
			ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			b.agent.ReportInterruptedTasks(ctx)
		}()
	})
}

// checkAmbient evaluates the ambient gate for a channel after a human
// message lands, and fires an unprompted response if it passes. Runs in its
// own goroutine so it never delays message handling.
func (b *Bot) checkAmbient(channelID snowflake.ID) {
	if !b.config.AmbientEnabled || b.ambientGate == nil || b.ambientClassifier == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		since := time.Now().Add(-b.config.AmbientWindow)
		msgs, err := b.store.GetRecentMessagesSince(ctx, uint64(channelID), since, 200)
		if err != nil {
			slog.Warn("failed to load ambient window", "channel", channelID, "error", err)
			return
		}

		result := b.ambientGate.CheckGate(ctx, channelID, b.client.ID(), msgs)
		if err := b.ambientGate.EvalTouch(ctx, channelID); err != nil {
			slog.Warn("failed to record ambient eval", "channel", channelID, "error", err)
		}
		if !result.Passed {
			return
		}

		llmMsgs := make([]llm.Message, 0, len(msgs))
		for _, m := range msgs {
			name := m.AuthorName
			if m.IsBot {
				name += " (bot)"
			}
			llmMsgs = append(llmMsgs, llm.NewUserMessage(llm.TextPart(fmt.Sprintf("[%s]: %s", name, m.Content))))
		}

		classified, err := b.ambientClassifier.Classify(ctx, uint64(channelID), llmMsgs)
		if err != nil {
			slog.Warn("ambient classification failed", "channel", channelID, "error", err)
			return
		}
		if classified.Hook == "" {
			return
		}

		sentID, err := b.agent.HandleAmbient(ctx, channelID, classified.Hook)
		if err != nil {
			slog.Warn("ambient response failed", "channel", channelID, "error", err)
			return
		}

		if err := b.ambientGate.LogFire(ctx, channelID, classified.Score, classified.Hook); err != nil {
			slog.Warn("failed to log ambient fire", "channel", channelID, "error", err)
		}
		if err := b.ambientGate.UpdateState(ctx, channelID, classified.Score, classified.Hook, uint64(sentID)); err != nil {
			slog.Warn("failed to update ambient state", "channel", channelID, "error", err)
		}
	}()
}
