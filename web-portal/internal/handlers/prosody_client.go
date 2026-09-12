package handlers

import (
	"context"

	"github.com/sudo-ivan/snikketx/web-portal/internal/prosody"
)

// ProsodyClient is the subset of the Prosody HTTP client the handlers call.
// *prosody.Client satisfies it implicitly; tests substitute a fake.
type ProsodyClient interface {
	// OAuth client registration and tokens.
	IsClientRegistered() bool
	RegisterClient(ctx context.Context) error
	Login(ctx context.Context, address, password string) (*prosody.TokenInfo, error)
	Logout(ctx context.Context, token string) error
	ChangePassword(ctx context.Context, address, currentPassword, newPassword string) (*prosody.TokenInfo, error)

	// mod_rest account operations.
	TestSession(ctx context.Context, token, address string) (bool, error)
	GetServerVersion(ctx context.Context, token, address string) (string, error)
	GetUserInfo(ctx context.Context, token, address string, isAdmin bool) (*prosody.UserInfo, error)
	SetUserNickname(ctx context.Context, token, address, nickname string) error
	GetAvatar(ctx context.Context, token, address string, metadataOnly bool) (*prosody.Avatar, error)
	GetAvatarData(ctx context.Context, token, address, id string) ([]byte, error)
	SetUserAvatar(ctx context.Context, token, address string, data []byte, mimetype string) error
	SetNicknameAccessModel(ctx context.Context, token, address, accessModel string) error
	SetAvatarAccessModel(ctx context.Context, token, address, accessModel string) error
	SetVCardAccessModel(ctx context.Context, token, address, accessModel string) error
	GuessProfileAccessModel(ctx context.Context, token, address string) (string, error)

	// mod_admin_api accounts, invitations and circles.
	ListUsers(ctx context.Context, token string) ([]prosody.AdminUserInfo, error)
	GetUserByLocalpart(ctx context.Context, token, localpart string) (*prosody.AdminUserInfo, error)
	UpdateUser(ctx context.Context, token, localpart string, update prosody.UserUpdate) error
	EnableUserAccount(ctx context.Context, token, localpart string) error
	DisableUserAccount(ctx context.Context, token, localpart string) error
	GetUserDebugInfo(ctx context.Context, token, localpart string) (map[string]any, error)
	DeleteUserByLocalpart(ctx context.Context, token, localpart string) error
	ListInvites(ctx context.Context, token string) ([]prosody.AdminInviteInfo, error)
	GetInviteByID(ctx context.Context, token, id string) (*prosody.AdminInviteInfo, error)
	DeleteInvite(ctx context.Context, token, id string) error
	CreateAccountInvite(ctx context.Context, token string, opts prosody.AccountInviteOptions) (*prosody.AdminInviteInfo, error)
	CreateGroupInvite(ctx context.Context, token string, opts prosody.GroupInviteOptions) (*prosody.AdminInviteInfo, error)
	CreatePasswordResetInvite(ctx context.Context, token, localpart string, ttl int) (*prosody.AdminInviteInfo, error)
	CreateGroup(ctx context.Context, token, name string, createMUC bool) (*prosody.AdminGroupInfo, error)
	ListGroups(ctx context.Context, token string) ([]prosody.AdminGroupInfo, error)
	GetGroupByID(ctx context.Context, token, id string) (*prosody.AdminGroupInfo, error)
	UpdateGroup(ctx context.Context, token, id, newName string) error
	AddGroupMember(ctx context.Context, token, id, localpart string) error
	RemoveGroupMember(ctx context.Context, token, id, localpart string) error
	AddGroupChat(ctx context.Context, token, id, name string) error
	RemoveGroupChat(ctx context.Context, token, groupID, chatID string) error
	DeleteGroup(ctx context.Context, token, id string) error
	GetSystemMetrics(ctx context.Context, token string) (map[string]any, error)
	PostAnnouncement(ctx context.Context, token, body, recipients, selfAddress string) error
	ListAuditEvents(ctx context.Context, token string, limit int, query string) ([]prosody.AuditEvent, error)

	// mod_snikket_muc_api group chats.
	ListMUCRooms(ctx context.Context, token, query string) ([]prosody.MUCRoom, string, error)
	GetMUCRoom(ctx context.Context, token, localpart string) (*prosody.MUCRoom, error)
	CreateMUCRoom(ctx context.Context, token, name, localpart, description string, public bool) (*prosody.MUCRoom, error)
	DestroyMUCRoom(ctx context.Context, token, localpart string) error

	// mod_snikket_ops_api operations.
	ListClientDevices(ctx context.Context, token, user string) ([]prosody.ClientDevice, error)
	ListMyClientDevices(ctx context.Context, token string) ([]prosody.ClientDevice, error)
	RevokeClientDevice(ctx context.Context, token, user, clientID string) error
	RevokeMyClientDevice(ctx context.Context, token, clientID string) error
	RevokeAllMyClientDevices(ctx context.Context, token string) error
	GetUploadsInfo(ctx context.Context, token string) (*prosody.UploadsInfo, error)
	PurgeUploads(ctx context.Context, token, mode, user string) (int, error)
	GetInviteStats(ctx context.Context, token string) (*prosody.InviteStats, error)
	GetArchivesInfo(ctx context.Context, token string) (*prosody.ArchivesInfo, error)
	GetUpdatesInfo(ctx context.Context, token string) (*prosody.UpdatesInfo, error)
	ExportAccountPackage(ctx context.Context, token, username string) ([]byte, error)
	ImportAccountPackage(ctx context.Context, token, username string, packageJSON []byte) error

	// mod_register_api and mod_xep227 public and migration endpoints.
	GetPublicInviteByID(ctx context.Context, id string) (*prosody.PublicInviteInfo, error)
	RegisterWithToken(ctx context.Context, inviteToken, username, password string) (string, error)
	ExportAccountData(ctx context.Context, token string) (string, error)
	ImportAccountData(ctx context.Context, token, userXML string) error
}

var _ ProsodyClient = (*prosody.Client)(nil)
