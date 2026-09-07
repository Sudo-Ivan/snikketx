// Package backupclient talks to the snikket_backup service.
package backupclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to the snikket_backup service.
type Client struct {
	Endpoint string
	Token    string
	HTTP     *http.Client
}

// JobStep is one progress step inside a backup job.
type JobStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	At     string `json:"at"`
}

// Job is an async backup run.
type Job struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Status  string    `json:"status"`
	Phase   string    `json:"phase"`
	Steps   []JobStep `json:"steps"`
	Log     []string  `json:"log"`
	Error   string    `json:"error"`
	Started string    `json:"started"`
	Ended   string    `json:"ended"`
}

// Archive is one local snikketx-backup-* directory.
type Archive struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Created   string `json:"created"`
	HasData   bool   `json:"has_data"`
	HasFull   bool   `json:"has_full"`
}

// Settings controls schedule, retention and restic.
type Settings struct {
	Enabled               bool   `json:"enabled"`
	IntervalHours         int    `json:"interval_hours"`
	Scope                 string `json:"scope"`
	KeepCount             int    `json:"keep_count"`
	KeepDays              int    `json:"keep_days"`
	ResticEnabled         bool   `json:"restic_enabled"`
	ResticKeepLast        int    `json:"restic_keep_last"`
	ResticKeepDaily       int    `json:"restic_keep_daily"`
	IncludeACMEChallenges bool   `json:"include_acme_challenges"`
}

// Status is the backup service status document.
type Status struct {
	Status                string    `json:"status"`
	Phase                 string    `json:"phase"`
	Configured            bool      `json:"configured"`
	Enabled               bool      `json:"enabled"`
	IntervalHours         int       `json:"interval_hours"`
	Scope                 string    `json:"scope"`
	KeepCount             int       `json:"keep_count"`
	KeepDays              int       `json:"keep_days"`
	ResticKeepLast        int       `json:"restic_keep_last"`
	ResticKeepDaily       int       `json:"restic_keep_daily"`
	IncludeACMEChallenges bool      `json:"include_acme_challenges"`
	ArchiveDir            string    `json:"archive_dir"`
	LastBackup            string    `json:"last_backup"`
	NextDue               string    `json:"next_due"`
	Detail                string    `json:"detail"`
	ResticEnabled         bool      `json:"restic_enabled"`
	ResticRepoSet         bool      `json:"restic_repo_set"`
	ArchiveCount          int       `json:"archive_count"`
	Job                   *Job      `json:"job"`
	RecentArchives        []Archive `json:"recent_archives"`
}

// DryRun is the restore dry-run response.
type DryRun struct {
	OK       bool     `json:"ok"`
	Archive  string   `json:"archive"`
	Messages []string `json:"messages"`
	Files    []string `json:"files"`
}

// Enabled reports whether the portal should call the backup service.
func (c *Client) Enabled() bool {
	return c != nil && strings.TrimSpace(c.Endpoint) != "" && strings.TrimSpace(c.Token) != ""
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	if !c.Enabled() {
		return 0, fmt.Errorf("backup service not configured")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Endpoint, "/")+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		return resp.StatusCode, fmt.Errorf("backup: %s", msg)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.Unmarshal(data, out)
}

// Status fetches backup service status.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var out Status
	if _, err := c.do(ctx, http.MethodGet, "/v1/status", nil, &out); err != nil {
		return nil, err
	}
	out.Configured = true
	return &out, nil
}

// SaveSettings stores backup settings.
func (c *Client) SaveSettings(ctx context.Context, settings Settings) error {
	_, err := c.do(ctx, http.MethodPut, "/v1/settings", settings, &settings)
	return err
}

// RunBackup starts an async backup.
func (c *Client) RunBackup(ctx context.Context) (*Status, error) {
	var out Status
	if _, err := c.do(ctx, http.MethodPost, "/v1/backup", nil, &out); err != nil {
		return nil, err
	}
	out.Configured = true
	return &out, nil
}

// Archives lists local archives.
func (c *Client) Archives(ctx context.Context) ([]Archive, error) {
	var out struct {
		Archives []Archive `json:"archives"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/v1/archives", nil, &out); err != nil {
		return nil, err
	}
	return out.Archives, nil
}

// DryRunRestore validates an archive without writing.
func (c *Client) DryRunRestore(ctx context.Context, archive string) (*DryRun, error) {
	var out DryRun
	if _, err := c.do(ctx, http.MethodPost, "/v1/restore/dry-run", map[string]string{"archive": archive}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResticCheck probes repository connectivity.
func (c *Client) ResticCheck(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/v1/restic/check", nil, nil)
	return err
}

// Healthz probes liveness without auth.
func (c *Client) Healthz(ctx context.Context) error {
	if !c.Enabled() {
		return fmt.Errorf("backup service not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Endpoint, "/")+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("backup healthz: %s", resp.Status)
	}
	return nil
}
