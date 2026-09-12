package handlers

// User-facing strings that appear in more than one place.
const (
	// errCredentials is the wording shown for every rejected login, so a
	// failed attempt never reveals whether the account exists.
	errCredentials         = "Invalid username or password."
	msgAlreadyExists       = "That already exists."
	msgAvatarTooBig        = "The chosen avatar is too big. To upload larger avatars, use your chat app."
	msgBackupNotConfigured = "Backup service is not configured."
	msgCircleNameRequired  = "A circle name is required."
	msgGroupChatDestroyed  = "Group chat destroyed."
	msgIncorrectPassword   = "Incorrect password."
	msgInviteRevoked       = "Invitation revoked."
	msgNoSuchCircle        = "No such circle exists."
	msgPasswordTooShort    = "The password must be at least 10 characters long."
	msgPasswordsMustMatch  = "The passwords must match."
	msgResetLinkNotFound   = "Password reset link not found."
	msgUnknownAction       = "Unknown action."
)
