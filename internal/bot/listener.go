package bot

import (
	"log/slog"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

type MessageRule struct {
	Predicate func(string) bool
	Response  string
}

var rules = []MessageRule{
	{
		Predicate: func(message string) bool {
			return strings.Contains(message, "67")
		},
		Response: "676767676767",
	},
	{
		Predicate: func(message string) bool {
			return strings.Contains(strings.ToLower(message), "oha")
		},
		Response: "Ladung?",
	},
}

func matchMessage(message string, rules []MessageRule) (string, bool) {
	for _, rule := range rules {
		if rule.Predicate(message) {
			return rule.Response, true
		}
	}
	return "", false
}

func onMessageCreate(event *events.MessageCreate) {
	if event.Message.Author.Bot {
		return
	}
	message, exists := matchMessage(event.Message.Content, rules)
	if exists {
		_, err := event.Client().Rest.CreateMessage(event.ChannelID, discord.NewMessageCreate().WithContent(message))
		if err != nil {
			slog.Warn("failed to send auto message", "error", err)
		}
	}
}
