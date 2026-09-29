package archive

import (
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

type filter struct {
	denyList string
}

// keepLive archives Rick's own messages so the ambient gate can see whether he already spoke in a burst.
func (f filter) keepLive(msg discord.Message, channelID snowflake.ID, selfID snowflake.ID) bool {
	if msg.Author.Bot && msg.Author.ID != selfID {
		return false
	}
	return f.keepContent(msg, channelID)
}

// keepBackfill archives other bots but never Rick.
func (f filter) keepBackfill(msg discord.Message, channelID snowflake.ID, selfID snowflake.ID) bool {
	if msg.Author.ID == selfID {
		return false
	}
	return f.keepContent(msg, channelID)
}

func (f filter) keepContent(msg discord.Message, channelID snowflake.ID) bool {
	if strings.HasPrefix(msg.Content, "/") {
		return false
	}
	return !isChannelDenied(f.denyList, channelID)
}

func isChannelDenied(denyList string, channelID snowflake.ID) bool {
	if denyList == "" {
		return false
	}
	denied := strings.SplitSeq(denyList, ",")
	for d := range denied {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if d == channelID.String() {
			return true
		}
	}
	return false
}
