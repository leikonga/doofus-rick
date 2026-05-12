package bot

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
)

func (b *Bot) buildUserRoster(overwrites discord.PermissionOverwrites) string {
	members := b.onlineMembers()
	if len(members) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<users>\n")
	for _, m := range members {
		if !memberCanSeeChannel(m, overwrites) {
			continue
		}
		fmt.Fprintf(&sb, "%s <@%s>", m.EffectiveName(), m.User.ID)
		if val, ok := b.voiceChannels.Load(m.User.ID); ok {
			if ch := val.(string); ch != "" {
				fmt.Fprintf(&sb, " (in VC: %s)", ch)
			} else {
				sb.WriteString(" (in VC)")
			}
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("</users>")
	return sb.String()
}

// memberCanSeeChannel checks channel permission overwrites to determine visibility.
// It does not account for base role permissions (guild-level), which requires
// fetching the full guild. For channels with no restrictive overwrites (the common
// case), all members pass through, which is correct.
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
