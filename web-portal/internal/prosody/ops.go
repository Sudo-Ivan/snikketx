package prosody

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ClientDevice is one registered client or push endpoint for an account.
type ClientDevice struct {
	User        string `json:"user"`
	ClientID    string `json:"client_id"`
	Name        string `json:"name"`
	UserAgent   string `json:"user_agent"`
	Resource    string `json:"resource"`
	IP          string `json:"ip"`
	FirstSeen   any    `json:"first_seen"`
	LastSeen    any    `json:"last_seen"`
	HasPush     bool   `json:"has_push"`
	PushService string `json:"push_service"`
}

// UploadFile is one HTTP upload blob summary.
type UploadFile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Uploader string `json:"uploader"`
	When     int64  `json:"when"`
}

// UploadsInfo is the share storage summary from mod_snikket_ops_api.
type UploadsInfo struct {
	Host          string         `json:"host"`
	Available     bool           `json:"available"`
	UsedBytes     int64          `json:"used_bytes"`
	FileCount     int            `json:"file_count"`
	OrphanCount   int            `json:"orphan_count"`
	Largest       []UploadFile   `json:"largest"`
	Orphans       []UploadFile   `json:"orphans"`
	Stats         map[string]any `json:"stats"`
	GlobalQuotaGB *float64       `json:"global_quota_gb"`
	DailyQuotaGB  *float64       `json:"daily_quota_gb"`
	RetentionDays int            `json:"retention_days"`
}

// InviteStats summarises invitation conversion and sources.
type InviteStats struct {
	Outstanding    int             `json:"outstanding"`
	Used           int             `json:"used"`
	ConversionRate float64         `json:"conversion_rate"`
	BySource       map[string]int  `json:"by_source"`
	Tracking       []InviteTrack   `json:"tracking"`
	Bootstrap      InviteBootstrap `json:"bootstrap"`
}

// InviteTrack is one used invitation row.
type InviteTrack struct {
	Token  string `json:"token"`
	Source string `json:"source"`
	When   any    `json:"when"`
	User   string `json:"user"`
}

// InviteBootstrap is bootstrap invite configuration state.
type InviteBootstrap struct {
	Records    int  `json:"records"`
	Configured bool `json:"configured"`
	Index      *int `json:"index"`
}

// ArchiveUser is per-account MAM and offline queue volume.
type ArchiveUser struct {
	Username      string `json:"username"`
	MAM           int    `json:"mam"`
	Offline       int    `json:"offline"`
	MAMOldest     *int64 `json:"mam_oldest"`
	OfflineOldest *int64 `json:"offline_oldest"`
}

// ArchivesInfo is the archive health summary.
type ArchivesInfo struct {
	MAMTotal        int           `json:"mam_total"`
	OfflineTotal    int           `json:"offline_total"`
	MUCMAMAvailable bool          `json:"muc_mam_available"`
	Users           []ArchiveUser `json:"users"`
	RetentionDays   int           `json:"retention_days"`
}

// UpdatesInfo is the update check / notify status.
type UpdatesInfo struct {
	Current       UpdatesCurrent `json:"current"`
	Branch        string         `json:"branch"`
	Latest        any            `json:"latest"`
	Secure        any            `json:"secure"`
	Message       any            `json:"message"`
	SupportStatus any            `json:"support_status"`
	CheckEnabled  bool           `json:"check_enabled"`
}

// UpdatesCurrent is the installed software identity.
type UpdatesCurrent struct {
	Version string `json:"version"`
	Branch  string `json:"branch"`
	Level   string `json:"level"`
}

// DisplayLatest returns a printable latest version or a dash.
func (u *UpdatesInfo) DisplayLatest() string {
	return displayAny(u.Latest)
}

// DisplaySecure returns a printable secure version or a dash.
func (u *UpdatesInfo) DisplaySecure() string {
	return displayAny(u.Secure)
}

// DisplayMessage returns a printable message or a dash.
func (u *UpdatesInfo) DisplayMessage() string {
	return displayAny(u.Message)
}

// DisplaySupport returns a printable support status or a dash.
func (u *UpdatesInfo) DisplaySupport() string {
	return displayAny(u.SupportStatus)
}

// DisplayCurrent returns the installed version string.
func (u *UpdatesInfo) DisplayCurrent() string {
	if u == nil {
		return "-"
	}
	if strings.TrimSpace(u.Current.Version) != "" {
		return u.Current.Version
	}
	if strings.TrimSpace(u.Current.Branch) != "" && strings.TrimSpace(u.Current.Level) != "" {
		return u.Current.Branch + " " + u.Current.Level
	}
	if strings.TrimSpace(u.Current.Branch) != "" {
		return u.Current.Branch
	}
	return "-"
}

func displayAny(value any) string {
	if value == nil {
		return "-"
	}
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "-"
		}
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == "<nil>" {
			return "-"
		}
		return s
	}
}

func (c *Client) opsEndpoint(parts ...string) string {
	path := c.Endpoint + "/snikket_ops_api"
	for _, part := range parts {
		path += "/" + strings.Trim(part, "/")
	}
	return path
}

// ListClientDevices returns registered clients across accounts.
func (c *Client) ListClientDevices(ctx context.Context, token, user string) ([]ClientDevice, error) {
	endpoint := c.opsEndpoint("clients")
	if user != "" {
		endpoint += "?user=" + url.QueryEscape(user)
	}
	resp, err := c.requestJSON(ctx, http.MethodGet, endpoint, token, nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if err := errorFromResponse(resp); err != nil {
		return nil, err
	}
	var payload struct {
		Clients []ClientDevice `json:"clients"`
	}
	if err := decodeJSONBody(resp, &payload); err != nil {
		return nil, err
	}
	return payload.Clients, nil
}

// RevokeClientDevice removes a client id and its push registration.
func (c *Client) RevokeClientDevice(ctx context.Context, token, user, clientID string) error {
	endpoint := c.opsEndpoint("clients", url.PathEscape(user), url.PathEscape(clientID))
	resp, err := c.requestJSON(ctx, http.MethodDelete, endpoint, token, nil)
	if err != nil {
		return err
	}
	defer closeBody(resp)
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return errorFromResponse(resp)
}

// GetUploadsInfo returns HTTP upload storage stats.
func (c *Client) GetUploadsInfo(ctx context.Context, token string) (*UploadsInfo, error) {
	resp, err := c.requestJSON(ctx, http.MethodGet, c.opsEndpoint("uploads"), token, nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)
	if resp.StatusCode == http.StatusNotFound {
		return &UploadsInfo{}, nil
	}
	if err := errorFromResponse(resp); err != nil {
		return nil, err
	}
	var info UploadsInfo
	if err := decodeJSONBody(resp, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// PurgeUploads deletes orphaned or expired upload metadata rows.
func (c *Client) PurgeUploads(ctx context.Context, token, mode string) (int, error) {
	if mode == "" {
		mode = "orphans"
	}
	endpoint := c.opsEndpoint("uploads/purge") + "?mode=" + url.QueryEscape(mode)
	resp, err := c.requestJSON(ctx, http.MethodPost, endpoint, token, nil)
	if err != nil {
		return 0, err
	}
	defer closeBody(resp)
	if resp.StatusCode == http.StatusNotFound {
		return 0, nil
	}
	if err := errorFromResponse(resp); err != nil {
		return 0, err
	}
	var payload struct {
		Removed int `json:"removed"`
	}
	if err := decodeJSONBody(resp, &payload); err != nil {
		return 0, err
	}
	return payload.Removed, nil
}

// GetInviteStats returns invitation analytics.
func (c *Client) GetInviteStats(ctx context.Context, token string) (*InviteStats, error) {
	resp, err := c.requestJSON(ctx, http.MethodGet, c.opsEndpoint("invites/stats"), token, nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)
	if resp.StatusCode == http.StatusNotFound {
		return &InviteStats{BySource: map[string]int{}}, nil
	}
	if err := errorFromResponse(resp); err != nil {
		return nil, err
	}
	var stats InviteStats
	if err := decodeJSONBody(resp, &stats); err != nil {
		return nil, err
	}
	if stats.BySource == nil {
		stats.BySource = map[string]int{}
	}
	return &stats, nil
}

// GetArchivesInfo returns MAM and offline queue volume.
func (c *Client) GetArchivesInfo(ctx context.Context, token string) (*ArchivesInfo, error) {
	resp, err := c.requestJSON(ctx, http.MethodGet, c.opsEndpoint("archives"), token, nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)
	if resp.StatusCode == http.StatusNotFound {
		return &ArchivesInfo{}, nil
	}
	if err := errorFromResponse(resp); err != nil {
		return nil, err
	}
	var info ArchivesInfo
	if err := decodeJSONBody(resp, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetUpdatesInfo returns update check status for the panel.
func (c *Client) GetUpdatesInfo(ctx context.Context, token string) (*UpdatesInfo, error) {
	resp, err := c.requestJSON(ctx, http.MethodGet, c.opsEndpoint("updates"), token, nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)
	if resp.StatusCode == http.StatusNotFound {
		return &UpdatesInfo{}, nil
	}
	if err := errorFromResponse(resp); err != nil {
		return nil, err
	}
	var info UpdatesInfo
	if err := decodeJSONBody(resp, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// ExportAccountPackage downloads the operator JSON account package.
func (c *Client) ExportAccountPackage(ctx context.Context, token, username string) ([]byte, error) {
	endpoint := c.opsEndpoint("export", url.PathEscape(username))
	resp, err := c.requestJSON(ctx, http.MethodGet, endpoint, token, nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)
	if err := errorFromResponse(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxIQBody))
}

// ImportAccountPackage restores roster and vcard from an operator package.
func (c *Client) ImportAccountPackage(ctx context.Context, token, username string, packageJSON []byte) error {
	var payload any
	if err := json.Unmarshal(packageJSON, &payload); err != nil {
		return err
	}
	endpoint := c.opsEndpoint("import", url.PathEscape(username))
	resp, err := c.requestJSON(ctx, http.MethodPut, endpoint, token, payload)
	if err != nil {
		return err
	}
	defer closeBody(resp)
	return errorFromResponse(resp)
}
