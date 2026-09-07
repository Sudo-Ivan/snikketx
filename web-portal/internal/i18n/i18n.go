package i18n

var en = map[string]string{
	"app.name":               "SnikketX",
	"nav.home":               "Home",
	"nav.users":              "Users",
	"nav.circles":            "Circles",
	"nav.mucs":               "Group chats",
	"nav.invites":            "Invitations",
	"nav.devices":            "Devices",
	"nav.storage":            "Storage",
	"nav.archives":           "Archives",
	"nav.certs":              "Certs and DNS",
	"nav.limits":             "Rate limits",
	"nav.updates":            "Updates",
	"nav.apps":               "Apps",
	"nav.backup":             "Backup",
	"nav.system":             "System",
	"nav.logs":               "Logs",
	"nav.health":             "Health",
	"nav.audit":              "Audit log",
	"nav.search":             "Search admin",
	"nav.search_placeholder": "Search pages and settings",
	"nav.profile":            "Profile",
	"nav.password":           "Password",
	"nav.data":               "Account data",
	"nav.logout":             "Log out",
	"nav.admin":              "Admin",
	"nav.exit_admin":         "Exit admin",
	"theme.toggle":           "Toggle light or dark mode",
	"login.title":            "Sign in",
	"login.address":          "Username",
	"login.password":         "Password",
	"login.submit":           "Sign in",
	"login.error":            "Invalid username or password.",
	"flash.login_ok":         "Login successful!",
	"flash.password_changed": "Password changed",
	"flash.profile_updated":  "Profile updated",
	"flash.user_updated":     "User information updated.",
	"flash.user_deleted":     "User deleted",
	"flash.invite_created":   "Invitation created",
	"flash.invite_revoked":   "Invitation revoked",
	"flash.circle_created":   "Circle created",
	"flash.announcement":     "Announcement sent!",
	"admin.welcome":          "Admin panel",
	"admin.ops_summary":      "Instance overview",
	"health.title":           "Health",
	"system.title":           "System",
	"metrics.unavailable":    "Metrics unavailable",
}

func T(key string) string {
	if v, ok := en[key]; ok {
		return v
	}
	return key
}
