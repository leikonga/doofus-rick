package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
)

const rosterActivityWindow = 90 * 24 * time.Hour

type rosterEntry struct {
	id       uint64
	name     string
	affinity int
	reason   string
}

// buildUserRoster returns the cached <leit> block (members ranked by archive
// activity, with affinity) and the uncached <grad do> block (live status for
// members of <leit> only). Both are scoped to members the requester can see
// in the channel.
func (a *Agent) buildUserRoster(ctx context.Context, overwrites discord.PermissionOverwrites) (leit string, gradDo string) {
	since := time.Now().Add(-rosterActivityWindow)
	actives, err := a.store.GetActiveAuthors(ctx, since, 200)
	if err != nil || len(actives) == 0 {
		return "", ""
	}

	var entries []rosterEntry
	for _, act := range actives {
		member, err := a.discord.GetMemberForID(strconv.FormatUint(act.AuthorID, 10))
		if err != nil || member == nil {
			continue
		}
		if !memberCanSeeChannel(*member, overwrites) {
			continue
		}

		name := member.EffectiveName()
		if name == "" {
			name = act.AuthorName
		}

		affinity := a.config.AffinityBaseline
		var reason string
		if a.affinity != nil {
			if res, err := a.affinity.Get(ctx, act.AuthorID); err == nil {
				affinity = res.Score
				reason = res.LastReason
			}
		}

		entries = append(entries, rosterEntry{id: act.AuthorID, name: name, affinity: affinity, reason: reason})
	}
	if len(entries) == 0 {
		return "", ""
	}

	return buildLeitBlock(entries), a.buildGradDoBlock(entries)
}

func buildLeitBlock(entries []rosterEntry) string {
	var sb strings.Builder
	sb.WriteString("<leit>\n")
	for _, e := range entries {
		fmt.Fprintf(&sb, "snowflake=%d name=%s affinity=%d", e.id, e.name, e.affinity)
		if e.reason != "" {
			fmt.Fprintf(&sb, " reason=%q", e.reason)
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("</leit>")
	return sb.String()
}

func (a *Agent) buildGradDoBlock(entries []rosterEntry) string {
	var sb strings.Builder
	sb.WriteString("<grad do>\n")
	wroteAny := false
	for _, e := range entries {
		idStr := strconv.FormatUint(e.id, 10)
		status := a.discord.GetStatusForID(idStr)
		if status == "" || status == discord.OnlineStatusOffline {
			continue
		}
		fmt.Fprintf(&sb, "snowflake=%d status=%s", e.id, status)
		if vc := a.discord.VoiceChannelForID(idStr); vc != "" {
			fmt.Fprintf(&sb, " vc=%s", vc)
		}
		if acts := a.discord.GetActivitiesForID(idStr); len(acts) > 0 {
			fmt.Fprintf(&sb, " activity=%s", acts[0].Name)
		}
		sb.WriteByte('\n')
		wroteAny = true
	}
	sb.WriteString("</grad do>")
	if !wroteAny {
		return ""
	}
	return sb.String()
}

func memberCanSeeChannel(member discord.Member, overwrites discord.PermissionOverwrites) bool {
	if len(overwrites) == 0 {
		return true
	}

	allow, deny := discord.PermissionsNone, discord.PermissionsNone

	for _, roleID := range member.RoleIDs {
		if ow, ok := overwrites.Role(roleID); ok {
			deny = deny.Add(ow.Deny)
			allow = allow.Add(ow.Allow)
		}
	}

	if ow, ok := overwrites.Member(member.User.ID); ok {
		deny = deny.Add(ow.Deny)
		allow = allow.Add(ow.Allow)
	}

	if deny.Has(discord.PermissionViewChannel) && !allow.Has(discord.PermissionViewChannel) {
		return false
	}
	return true
}
