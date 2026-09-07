package handlers

import (
	"net"
	"strings"
)

// sessionView is one signed-in client or the portal itself on the profile page.
type sessionView struct {
	ClientID    string
	Name        string
	UserAgent   string
	Resource    string
	IP          string
	MapURL      string
	LastSeen    any
	HasPush     bool
	PushService string
	Icon        string
	Kind        string
	IsPortal    bool
	IsCurrent   bool
}

func classifyClient(name, userAgent, resource string) (icon, kind, label string) {
	blob := strings.ToLower(strings.Join([]string{name, userAgent, resource}, " "))
	switch {
	case strings.Contains(blob, "snikket"):
		return "smartphone", "Snikket", firstNonEmpty(name, "Snikket")
	case strings.Contains(blob, "conversations"):
		return "smartphone", "Conversations", firstNonEmpty(name, "Conversations")
	case strings.Contains(blob, "monal"):
		return "smartphone", "Monal", firstNonEmpty(name, "Monal")
	case strings.Contains(blob, "dino"):
		return "server", "Dino", firstNonEmpty(name, "Dino")
	case strings.Contains(blob, "gajim"):
		return "server", "Gajim", firstNonEmpty(name, "Gajim")
	case strings.Contains(blob, "cheogram"):
		return "smartphone", "Cheogram", firstNonEmpty(name, "Cheogram")
	case strings.Contains(blob, "mozilla") || strings.Contains(blob, "chrome") || strings.Contains(blob, "safari") || strings.Contains(blob, "firefox"):
		return "eye", "Browser", firstNonEmpty(name, "Browser")
	case strings.Contains(blob, "portal") || strings.Contains(blob, "snikketx"):
		return "circle-user", "Web portal", firstNonEmpty(name, "Web portal")
	default:
		return "smartphone", "Client", firstNonEmpty(name, "Chat client")
	}
}

func ipMapURL(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "-" {
		return ""
	}
	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		ip = host
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	if parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsLinkLocalUnicast() {
		return ""
	}
	return "https://bgp.tools/" + ip
}
