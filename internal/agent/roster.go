package agent

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
)

func (a *Agent) buildUserRoster(overwrites discord.PermissionOverwrites) string {
	members := a.discord.OnlineMembers()
	if len(members) == 0 {
		return ""
	}
	vcs := a.discord.VoiceChannels()
	var sb strings.Builder
	sb.WriteString("<users>\n")
	for _, m := range members {
		if !memberCanSeeChannel(m, overwrites) {
			continue
		}
		fmt.Fprintf(&sb, "snowflake=%s username=%s display_name=%s", m.User.ID, m.User.Username, m.EffectiveName())
		if ch, ok := vcs[m.User.ID]; ok {
			if ch != "" {
				fmt.Fprintf(&sb, " vc=%s", ch)
			} else {
				sb.WriteString(" vc=unknown")
			}
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("</users>")
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
