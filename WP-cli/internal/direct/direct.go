package direct

import "go.mau.fi/whatsmeow/types"

// Is reports whether jid is a 1:1 chat. Groups, channels, broadcasts, and bots are excluded.
func Is(j types.JID) bool {
	j = Canonical(j)
	switch j.Server {
	case types.DefaultUserServer, types.HiddenUserServer, types.HostedServer, types.HostedLIDServer:
		return j.User != "" && !j.IsBot()
	default:
		return false
	}
}

// IsGroup reports whether jid is a group chat.
func IsGroup(j types.JID) bool {
	j = Canonical(j)
	return j.Server == types.GroupServer && j.User != ""
}

// Allowed reports whether a chat is stored: a direct chat or a group.
// Newsletters, status broadcasts, and bots stay excluded.
func Allowed(j types.JID) bool {
	return Is(j) || IsGroup(j)
}

// Canonical strips the device id and rewrites legacy user servers.
func Canonical(j types.JID) types.JID {
	j = j.ToNonAD()
	if j.Server == types.LegacyUserServer {
		j.Server = types.DefaultUserServer
	}
	return j
}

// Phone returns the E.164 digits when jid is a phone-number user, without a leading plus.
func Phone(j types.JID) string {
	j = Canonical(j)
	if j.Server != types.DefaultUserServer && j.Server != types.HostedServer {
		return ""
	}
	if j.User == "" {
		return ""
	}
	for _, r := range j.User {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return j.User
}
