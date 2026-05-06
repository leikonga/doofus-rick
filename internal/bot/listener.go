package bot

import (
	"log/slog"
	"regexp"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

var stripPattern = regexp.MustCompile(`<[@#&!][0-9]+>|https?://\S+`)

type messageRule struct {
	Predicate func(string) bool
	Response  string
}

var rules = []messageRule{
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

func matchMessage(message string, rules []messageRule) (string, bool) {
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
	botID := event.Client().ID()
	for _, u := range event.Message.Mentions {
		if u.ID == botID {
			return
		}
	}
	content := stripPattern.ReplaceAllString(event.Message.Content, "")
	message, exists := matchMessage(content, rules)
	if exists {
		_, err := event.Client().Rest.CreateMessage(event.ChannelID, discord.NewMessageCreate().WithContent(message))
		if err != nil {
			slog.Warn("failed to send auto message", "error", err)
		}
	}
}
