// Package prosody implements a client for the HTTP APIs exposed by a Snikket
// Prosody server: OAuth 2.0 token issuance, mod_rest, mod_admin_api,
// mod_register_api and mod_xep227.
package prosody

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

// OAuth 2.0 scopes understood by Prosody.
const (
	ScopeRestricted = "prosody:restricted"
	ScopeDefault    = "prosody:registered"
	ScopeAdmin      = "prosody:admin"
)

// Access models supported by the pubsub nodes that back a user profile,
// ordered from the most open to the most restrictive.
const (
	AccessModelOpen      = "open"
	AccessModelPresence  = "presence"
	AccessModelWhitelist = "whitelist"
)

// InviteType identifies the kind of invitation issued by the server.
type InviteType string

// Invitation kinds returned by mod_invites.
const (
	InviteTypeRegister InviteType = "register"
	InviteTypeRoster   InviteType = "roster"
)

// APIError is the structured error object returned by mod_admin_api inside an
// "error" envelope.
type APIError struct {
	Type      string            `json:"type"`
	Condition string            `json:"condition"`
	Text      string            `json:"text,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Text != "" {
		return fmt.Sprintf("%s: %s: %s", e.Type, e.Condition, e.Text)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Condition)
}

// HTTPError reports a non success response that carried no structured API
// error object.
type HTTPError struct {
	Status  int    `json:"status"`
	Message string `json:"message,omitempty"`
	Body    string `json:"body,omitempty"`
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("prosody: http %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("prosody: http %d", e.Status)
}

// StatusOf reports the HTTP status carried by err, or zero when err is not an
// HTTPError.
func StatusOf(err error) int {
	if httpErr, ok := errors.AsType[*HTTPError](err); ok {
		return httpErr.Status
	}
	if iqErr, ok := errors.AsType[*xmpp.IQError](err); ok {
		return iqErr.Status
	}
	return 0
}

// TokenInfo is a bearer token together with the scopes it was granted.
type TokenInfo struct {
	Token  string   `json:"access_token"`
	Scopes []string `json:"scopes"`
}

// HasScope reports whether the token carries the given scope.
func (t TokenInfo) HasScope(scope string) bool {
	return slices.Contains(t.Scopes, scope)
}

// IsAdmin reports whether the token carries the Prosody admin scope.
func (t TokenInfo) IsAdmin() bool {
	return t.HasScope(ScopeAdmin)
}

// tokenResponse is the wire form of an OAuth 2.0 token endpoint reply.
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	ExpiresIn        int64  `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// clientRegistrationRequest is the dynamic client registration payload sent to
// the OAuth 2.0 registration endpoint.
type clientRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri"`
	RedirectURIs            []string `json:"redirect_uris"`
	ApplicationType         string   `json:"application_type"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
	SoftwareID              string   `json:"software_id"`
	SoftwareVersion         string   `json:"software_version"`
}

// clientRegistrationResponse holds the credentials issued to a registered
// OAuth 2.0 client.
type clientRegistrationResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// UserDeletionRequestInfo describes a pending account deletion.
type UserDeletionRequestInfo struct {
	DeletedAt    time.Time `json:"deleted_at"`
	PendingUntil time.Time `json:"pending_until"`
}

// UnmarshalJSON decodes the Unix timestamps used on the wire.
func (u *UserDeletionRequestInfo) UnmarshalJSON(data []byte) error {
	var raw struct {
		DeletedAt    int64 `json:"deleted_at"`
		PendingUntil int64 `json:"pending_until"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	u.DeletedAt = time.Unix(raw.DeletedAt, 0).UTC()
	u.PendingUntil = time.Unix(raw.PendingUntil, 0).UTC()
	return nil
}

// AvatarMetadata describes one avatar variant advertised by the server.
type AvatarMetadata struct {
	Hash   string `json:"hash"`
	Bytes  int    `json:"bytes"`
	Type   string `json:"type"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// Avatar is an avatar as published on the user's pubsub nodes. Data is only
// populated when the full avatar was requested.
type Avatar struct {
	SHA1   string `json:"sha1"`
	Type   string `json:"type"`
	Bytes  int    `json:"bytes"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Data   []byte `json:"data,omitempty"`
}

// AdminUserInfo is a user account as seen through mod_admin_api.
type AdminUserInfo struct {
	Localpart       string                   `json:"username"`
	DisplayName     string                   `json:"display_name,omitempty"`
	Email           string                   `json:"email,omitempty"`
	Phone           string                   `json:"phone,omitempty"`
	Roles           []string                 `json:"roles,omitempty"`
	Enabled         bool                     `json:"enabled"`
	LastActive      *int64                   `json:"last_active,omitempty"`
	DeletionRequest *UserDeletionRequestInfo `json:"deletion_request,omitempty"`
	AvatarInfo      []AvatarMetadata         `json:"avatar_info,omitempty"`
}

// HasRole reports whether the account holds the given role.
func (u AdminUserInfo) HasRole(role string) bool {
	return slices.Contains(u.Roles, role)
}

// HasAdminRole reports whether the account holds the Prosody admin role.
func (u AdminUserInfo) HasAdminRole() bool {
	return u.HasRole(ScopeAdmin)
}

// HasRestrictedRole reports whether the account holds the restricted role.
func (u AdminUserInfo) HasRestrictedRole() bool {
	return u.HasRole(ScopeRestricted)
}

// UnmarshalJSON accepts both role shapes used by mod_admin_api: a primary
// "role" with optional "secondary_roles", or a flat "roles" array. Accounts
// are enabled unless the server says otherwise, and avatar entries without a
// hash are dropped as broken.
func (u *AdminUserInfo) UnmarshalJSON(data []byte) error {
	var raw struct {
		Username        string                   `json:"username"`
		DisplayName     string                   `json:"display_name"`
		Email           string                   `json:"email"`
		Phone           string                   `json:"phone"`
		Role            string                   `json:"role"`
		SecondaryRoles  []string                 `json:"secondary_roles"`
		Roles           []string                 `json:"roles"`
		Enabled         *bool                    `json:"enabled"`
		LastActive      *int64                   `json:"last_active"`
		DeletionRequest *UserDeletionRequestInfo `json:"deletion_request"`
		AvatarInfo      []AvatarMetadata         `json:"avatar_info"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	var roles []string
	if raw.Role != "" {
		roles = make([]string, 0, 1+len(raw.SecondaryRoles))
		roles = append(roles, raw.Role)
		roles = append(roles, raw.SecondaryRoles...)
	} else {
		roles = raw.Roles
	}

	avatars := make([]AvatarMetadata, 0, len(raw.AvatarInfo))
	for _, avatar := range raw.AvatarInfo {
		if avatar.Hash == "" {
			continue
		}
		avatars = append(avatars, avatar)
	}

	enabled := true
	if raw.Enabled != nil {
		enabled = *raw.Enabled
	}

	lastActive := raw.LastActive
	if lastActive != nil && *lastActive == 0 {
		lastActive = nil
	}

	u.Localpart = raw.Username
	u.DisplayName = raw.DisplayName
	u.Email = raw.Email
	u.Phone = raw.Phone
	u.Roles = roles
	u.Enabled = enabled
	u.LastActive = lastActive
	u.DeletionRequest = raw.DeletionRequest
	u.AvatarInfo = avatars
	return nil
}

// AdminInviteInfo is an invitation as seen through mod_admin_api.
type AdminInviteInfo struct {
	ID          string     `json:"id"`
	Type        InviteType `json:"type"`
	JID         string     `json:"jid,omitempty"`
	Token       string     `json:"token"`
	XMPPURI     string     `json:"xmpp_uri,omitempty"`
	LandingPage string     `json:"landing_page,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	Expires     time.Time  `json:"expires"`
	Reusable    bool       `json:"reusable"`
	GroupIDs    []string   `json:"groups,omitempty"`
	RoleNames   []string   `json:"roles,omitempty"`
	IsReset     bool       `json:"reset"`
	Note        string     `json:"note,omitempty"`
}

// UnmarshalJSON decodes the Unix timestamps used on the wire and mirrors the
// invite id into the token field.
func (i *AdminInviteInfo) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID          string   `json:"id"`
		Type        string   `json:"type"`
		JID         string   `json:"jid"`
		XMPPURI     string   `json:"xmpp_uri"`
		LandingPage string   `json:"landing_page"`
		CreatedAt   int64    `json:"created_at"`
		Expires     int64    `json:"expires"`
		Reusable    bool     `json:"reusable"`
		Groups      []string `json:"groups"`
		Roles       []string `json:"roles"`
		Reset       bool     `json:"reset"`
		Note        string   `json:"note"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	i.ID = raw.ID
	i.Type = InviteType(raw.Type)
	i.JID = raw.JID
	i.Token = raw.ID
	i.XMPPURI = raw.XMPPURI
	i.LandingPage = raw.LandingPage
	i.CreatedAt = time.Unix(raw.CreatedAt, 0).UTC()
	i.Expires = time.Unix(raw.Expires, 0).UTC()
	i.Reusable = raw.Reusable
	i.GroupIDs = raw.Groups
	i.RoleNames = raw.Roles
	i.IsReset = raw.Reset
	i.Note = raw.Note
	return nil
}

// AdminGroupChatInfo is a group chat attached to a circle.
type AdminGroupChatInfo struct {
	ID   string `json:"id"`
	JID  string `json:"jid"`
	Name string `json:"name"`
}

// AdminGroupInfo is a circle as seen through mod_admin_api.
type AdminGroupInfo struct {
	ID      string               `json:"id"`
	Name    string               `json:"name"`
	Members []string             `json:"members,omitempty"`
	Chats   []AdminGroupChatInfo `json:"chats,omitempty"`
}

// PublicInviteInfo is the unauthenticated view of an invitation offered by
// mod_register_api.
type PublicInviteInfo struct {
	Inviter        string `json:"inviter,omitempty"`
	XMPPURI        string `json:"uri"`
	ResetLocalpart string `json:"reset,omitempty"`
	Domain         string `json:"domain"`
}

// UserInfo is the summary of the logged in account shown by the portal.
type UserInfo struct {
	Address     string `json:"address"`
	Username    string `json:"username"`
	Nickname    string `json:"nickname,omitempty"`
	DisplayName string `json:"display_name"`
	AvatarHash  string `json:"avatar_hash,omitempty"`
	IsAdmin     bool   `json:"is_admin"`
}

// AccountInviteOptions carries the optional parameters accepted when creating
// an account invitation. Zero values are omitted from the request.
type AccountInviteOptions struct {
	GroupIDs         []string
	RoleNames        []string
	RestrictUsername string
	TTL              int
	Note             string
}

// GroupInviteOptions carries the optional parameters accepted when creating a
// group invitation. Zero values are omitted from the request.
type GroupInviteOptions struct {
	GroupIDs  []string
	RoleNames []string
	TTL       int
	Note      string
}

// UserUpdate carries the fields that may be changed on an existing account.
// Nil fields are left untouched.
type UserUpdate struct {
	DisplayName *string
	Role        *string
}
