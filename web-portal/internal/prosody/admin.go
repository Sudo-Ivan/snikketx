package prosody

import (
	"context"
	"net/http"
)

// ListUsers returns every account on the virtual host.
func (c *Client) ListUsers(ctx context.Context, token string) ([]AdminUserInfo, error) {
	var users []AdminUserInfo
	if err := c.callJSON(ctx, http.MethodGet, c.adminEndpoint("users"), token, nil, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// GetUserByLocalpart returns a single account.
func (c *Client) GetUserByLocalpart(ctx context.Context, token, localpart string) (*AdminUserInfo, error) {
	var user AdminUserInfo
	if err := c.callJSON(ctx, http.MethodGet, c.adminEndpoint("users", localpart), token, nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateUser changes the display name or the role of an account. Fields left
// nil in the update are not sent.
func (c *Client) UpdateUser(ctx context.Context, token, localpart string, update UserUpdate) error {
	payload := map[string]any{
		"username": localpart,
	}
	if update.DisplayName != nil {
		payload["display_name"] = *update.DisplayName
	}
	if update.Role != nil {
		payload["role"] = *update.Role
	}
	return c.callJSON(ctx, http.MethodPut, c.adminEndpoint("users", localpart), token, payload, nil)
}

// EnableUserAccount re-enables a disabled account.
func (c *Client) EnableUserAccount(ctx context.Context, token, localpart string) error {
	payload := map[string]any{"enabled": true}
	return c.callJSON(ctx, http.MethodPatch, c.adminEndpoint("users", localpart), token, payload, nil)
}

// DisableUserAccount blocks an account without deleting it.
func (c *Client) DisableUserAccount(ctx context.Context, token, localpart string) error {
	payload := map[string]any{"enabled": false}
	return c.callJSON(ctx, http.MethodPatch, c.adminEndpoint("users", localpart), token, payload, nil)
}

// GetUserDebugInfo returns the raw diagnostic document for an account.
func (c *Client) GetUserDebugInfo(ctx context.Context, token, localpart string) (map[string]any, error) {
	var info map[string]any
	if err := c.callJSON(ctx, http.MethodGet, c.adminEndpoint("users", localpart, "debug"), token, nil, &info); err != nil {
		return nil, err
	}
	return info, nil
}

// DeleteUserByLocalpart removes an account.
func (c *Client) DeleteUserByLocalpart(ctx context.Context, token, localpart string) error {
	return c.callJSON(ctx, http.MethodDelete, c.adminEndpoint("users", localpart), token, nil, nil)
}

// ListInvites returns every outstanding invitation.
func (c *Client) ListInvites(ctx context.Context, token string) ([]AdminInviteInfo, error) {
	var invites []AdminInviteInfo
	if err := c.callJSON(ctx, http.MethodGet, c.adminEndpoint("invites"), token, nil, &invites); err != nil {
		return nil, err
	}
	return invites, nil
}

// GetInviteByID returns a single invitation.
func (c *Client) GetInviteByID(ctx context.Context, token, id string) (*AdminInviteInfo, error) {
	var invite AdminInviteInfo
	if err := c.callJSON(ctx, http.MethodGet, c.adminEndpoint("invites", id), token, nil, &invite); err != nil {
		return nil, err
	}
	return &invite, nil
}

// DeleteInvite revokes an invitation.
func (c *Client) DeleteInvite(ctx context.Context, token, id string) error {
	return c.callJSON(ctx, http.MethodDelete, c.adminEndpoint("invites", id), token, nil, nil)
}

// CreateAccountInvite issues an invitation that creates a new account.
func (c *Client) CreateAccountInvite(ctx context.Context, token string, opts AccountInviteOptions) (*AdminInviteInfo, error) {
	payload := map[string]any{
		"groups": stringsOrEmpty(opts.GroupIDs),
		"roles":  stringsOrEmpty(opts.RoleNames),
	}
	if opts.RestrictUsername != "" {
		payload["username"] = opts.RestrictUsername
	}
	if opts.TTL > 0 {
		payload["ttl"] = opts.TTL
	}
	if opts.Note != "" {
		payload["note"] = opts.Note
	}

	var invite AdminInviteInfo
	if err := c.callJSON(ctx, http.MethodPost, c.adminEndpoint("invites", "account"), token, payload, &invite); err != nil {
		return nil, err
	}
	return &invite, nil
}

// CreateGroupInvite issues an invitation that adds an existing contact to a
// set of circles.
func (c *Client) CreateGroupInvite(ctx context.Context, token string, opts GroupInviteOptions) (*AdminInviteInfo, error) {
	payload := map[string]any{
		"groups": stringsOrEmpty(opts.GroupIDs),
		"roles":  stringsOrEmpty(opts.RoleNames),
	}
	if opts.TTL > 0 {
		payload["ttl"] = opts.TTL
	}
	if opts.Note != "" {
		payload["note"] = opts.Note
	}

	var invite AdminInviteInfo
	if err := c.callJSON(ctx, http.MethodPost, c.adminEndpoint("invites", "group"), token, payload, &invite); err != nil {
		return nil, err
	}
	return &invite, nil
}

// CreatePasswordResetInvite issues a single use password reset link for an
// account. A ttl of zero leaves the server default in place.
func (c *Client) CreatePasswordResetInvite(ctx context.Context, token, localpart string, ttl int) (*AdminInviteInfo, error) {
	payload := map[string]any{
		"username": localpart,
	}
	if ttl > 0 {
		payload["ttl"] = ttl
	}

	var invite AdminInviteInfo
	if err := c.callJSON(ctx, http.MethodPost, c.adminEndpoint("invites", "reset"), token, payload, &invite); err != nil {
		return nil, err
	}
	return &invite, nil
}

// CreateGroup creates a circle, optionally with a group chat attached.
func (c *Client) CreateGroup(ctx context.Context, token, name string, createMUC bool) (*AdminGroupInfo, error) {
	payload := map[string]any{
		"name":       name,
		"create_muc": createMUC,
	}

	var group AdminGroupInfo
	if err := c.callJSON(ctx, http.MethodPost, c.adminEndpoint("groups"), token, payload, &group); err != nil {
		return nil, err
	}
	return &group, nil
}

// ListGroups returns every circle on the virtual host.
func (c *Client) ListGroups(ctx context.Context, token string) ([]AdminGroupInfo, error) {
	var groups []AdminGroupInfo
	if err := c.callJSON(ctx, http.MethodGet, c.adminEndpoint("groups"), token, nil, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

// GetGroupByID returns a single circle.
func (c *Client) GetGroupByID(ctx context.Context, token, id string) (*AdminGroupInfo, error) {
	var group AdminGroupInfo
	if err := c.callJSON(ctx, http.MethodGet, c.adminEndpoint("groups", id), token, nil, &group); err != nil {
		return nil, err
	}
	return &group, nil
}

// UpdateGroup renames a circle. An empty name leaves it unchanged.
func (c *Client) UpdateGroup(ctx context.Context, token, id, newName string) error {
	payload := map[string]any{}
	if newName != "" {
		payload["name"] = newName
	}
	return c.callJSON(ctx, http.MethodPut, c.adminEndpoint("groups", id), token, payload, nil)
}

// AddGroupMember adds an account to a circle.
func (c *Client) AddGroupMember(ctx context.Context, token, id, localpart string) error {
	endpoint := c.adminEndpoint("groups", id, "members", localpart)
	return c.callJSON(ctx, http.MethodPut, endpoint, token, nil, nil)
}

// RemoveGroupMember removes an account from a circle.
func (c *Client) RemoveGroupMember(ctx context.Context, token, id, localpart string) error {
	endpoint := c.adminEndpoint("groups", id, "members", localpart)
	return c.callJSON(ctx, http.MethodDelete, endpoint, token, nil, nil)
}

// AddGroupChat creates a group chat inside a circle.
func (c *Client) AddGroupChat(ctx context.Context, token, id, name string) error {
	payload := map[string]any{"name": name}
	return c.callJSON(ctx, http.MethodPost, c.adminEndpoint("groups", id, "chats"), token, payload, nil)
}

// RemoveGroupChat deletes a group chat from a circle.
func (c *Client) RemoveGroupChat(ctx context.Context, token, groupID, chatID string) error {
	endpoint := c.adminEndpoint("groups", groupID, "chats", chatID)
	return c.callJSON(ctx, http.MethodDelete, endpoint, token, nil, nil)
}

// DeleteGroup removes a circle.
func (c *Client) DeleteGroup(ctx context.Context, token, id string) error {
	return c.callJSON(ctx, http.MethodDelete, c.adminEndpoint("groups", id), token, nil, nil)
}

// GetSystemMetrics returns the server metrics document. Servers without the
// metrics module answer 404, which is reported as an empty map rather than an
// error.
func (c *Client) GetSystemMetrics(ctx context.Context, token string) (map[string]any, error) {
	resp, err := c.requestJSON(ctx, http.MethodGet, c.adminEndpoint("server", "metrics"), token, nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	if resp.StatusCode == http.StatusNotFound {
		return map[string]any{}, nil
	}
	if err := errorFromResponse(resp); err != nil {
		return nil, err
	}

	metrics := map[string]any{}
	if resp.StatusCode == http.StatusNoContent {
		return metrics, nil
	}
	if err := decodeJSONBody(resp, &metrics); err != nil {
		return nil, err
	}
	return metrics, nil
}

// PostAnnouncement sends a server announcement. The recipients value is passed
// through unchanged, except for "self" which is expanded to the caller's own
// address.
func (c *Client) PostAnnouncement(ctx context.Context, token, body, recipients, selfAddress string) error {
	var recipientsPayload any = recipients
	if recipients == "self" {
		recipientsPayload = []string{selfAddress}
	}

	payload := map[string]any{
		"recipients": recipientsPayload,
		"body":       body,
	}
	return c.callJSON(ctx, http.MethodPost, c.adminEndpoint("server", "announcement"), token, payload, nil)
}

// stringsOrEmpty normalises a nil slice so it serialises as an empty JSON
// array instead of null.
func stringsOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
