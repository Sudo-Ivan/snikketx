package updater

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to the snikket_updater service.
type Client struct {
	Endpoint string
	Token    string
	HTTP     *http.Client
}

// JobStep is one progress step inside an updater job.
type JobStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	At     string `json:"at"`
}

// Job is an async check or apply run.
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

// Service is one compose service image row.
type Service struct {
	Name            string `json:"name"`
	Image           string `json:"image"`
	Digest          string `json:"digest"`
	PinnedDigest    string `json:"pinned_digest"`
	PinnedImage     string `json:"pinned_image"`
	UpdateAvailable bool   `json:"update_available"`
	SignatureOK     *bool  `json:"signature_ok"`
	SignatureDetail string `json:"signature_detail"`
	Registry        string `json:"registry"`
}

// SignatureState is verified, failed, or n/a when the image was not checked.
func (s Service) SignatureState() string {
	if s.SignatureOK == nil {
		return "n/a"
	}
	if *s.SignatureOK {
		return "verified"
	}
	return "failed"
}

// Settings controls check cadence, auto-apply and digest pinning.
type Settings struct {
	IntervalHours int  `json:"interval_hours"`
	AutoUpdate    bool `json:"auto_update"`
	PinDigests    bool `json:"pin_digests"`
}

// Status is the updater status document.
type Status struct {
	Status        string    `json:"status"`
	Phase         string    `json:"phase"`
	Available     bool      `json:"available"`
	LastCheck     string    `json:"last_check"`
	LastApply     string    `json:"last_apply"`
	IntervalHours int       `json:"interval_hours"`
	AutoUpdate    bool      `json:"auto_update"`
	PinDigests    bool      `json:"pin_digests"`
	Services      []Service `json:"services"`
	Detail        string    `json:"detail"`
	Configured    bool      `json:"configured"`
	ImagePrefix   string    `json:"image_prefix"`
	VerifyEnabled bool      `json:"verify_enabled"`
	RequireVerify bool      `json:"require_signatures"`
	Job           *Job      `json:"job"`
}

// Pins is the locked digest map.
type Pins struct {
	Services map[string]string `json:"services"`
}

// LogService is one allowlisted compose service for log viewing.
type LogService struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Logs is a capped container log tail from the updater.
type Logs struct {
	Service   string       `json:"service"`
	Label     string       `json:"label"`
	Tail      int          `json:"tail"`
	Lines     []string     `json:"lines"`
	Entries   []LogEntry   `json:"entries"`
	Truncated bool         `json:"truncated"`
	FetchedAt string       `json:"fetched_at"`
	Services  []LogService `json:"services"`
}

// LogEntry is one timestamped compose log line.
type LogEntry struct {
	Time    string `json:"time"`
	Display string `json:"display"`
	Text    string `json:"text"`
}

// Enabled reports whether the portal should call the updater.
func (c *Client) Enabled() bool {
	return c != nil && strings.TrimSpace(c.Endpoint) != "" && strings.TrimSpace(c.Token) != ""
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 45 * time.Second}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	if !c.Enabled() {
		return 0, fmt.Errorf("updater not configured")
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
		return resp.StatusCode, fmt.Errorf("updater: %s", msg)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.Unmarshal(data, out)
}

// Status fetches updater status.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var out Status
	if _, err := c.do(ctx, http.MethodGet, "/v1/status", nil, &out); err != nil {
		return nil, err
	}
	out.Configured = true
	if out.IntervalHours <= 0 {
		out.IntervalHours = 24
	}
	return &out, nil
}

// Check starts an async image check.
func (c *Client) Check(ctx context.Context) (*Status, error) {
	var out Status
	if _, err := c.do(ctx, http.MethodPost, "/v1/check", nil, &out); err != nil {
		return nil, err
	}
	out.Configured = true
	return &out, nil
}

// Apply starts an async compose apply.
func (c *Client) Apply(ctx context.Context) (*Status, error) {
	var out Status
	if _, err := c.do(ctx, http.MethodPost, "/v1/apply", nil, &out); err != nil {
		return nil, err
	}
	out.Configured = true
	return &out, nil
}

// SaveSettings stores updater settings.
func (c *Client) SaveSettings(ctx context.Context, settings Settings) error {
	_, err := c.do(ctx, http.MethodPut, "/v1/settings", settings, &settings)
	return err
}

// ClearPins removes digest locks.
func (c *Client) ClearPins(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodDelete, "/v1/pins", nil, nil)
	return err
}

// Logs fetches a capped log tail for an allowlisted compose service.
// Pass an empty service to list available services only.
func (c *Client) Logs(ctx context.Context, service string, tail int) (*Logs, error) {
	path := "/v1/logs"
	query := ""
	if strings.TrimSpace(service) != "" {
		query += "service=" + url.QueryEscape(strings.TrimSpace(service))
	}
	if tail > 0 {
		if query != "" {
			query += "&"
		}
		query += fmt.Sprintf("tail=%d", tail)
	}
	if query != "" {
		path += "?" + query
	}
	var out Logs
	if _, err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Healthz probes the updater liveness endpoint without auth.
func (c *Client) Healthz(ctx context.Context) error {
	if !c.Enabled() {
		return fmt.Errorf("updater not configured")
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
		return fmt.Errorf("updater healthz: %s", resp.Status)
	}
	return nil
}
