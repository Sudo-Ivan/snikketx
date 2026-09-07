package updater

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

// Client talks to the snikket_updater service.
type Client struct {
	Endpoint string
	Token    string
	HTTP     *http.Client
}

// Service is one compose service image row.
type Service struct {
	Name            string `json:"name"`
	Image           string `json:"image"`
	Digest          string `json:"digest"`
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

// Settings controls check cadence and auto-apply.
type Settings struct {
	IntervalHours int  `json:"interval_hours"`
	AutoUpdate    bool `json:"auto_update"`
}

// Status is the updater status document.
type Status struct {
	Status        string    `json:"status"`
	Available     bool      `json:"available"`
	LastCheck     string    `json:"last_check"`
	LastApply     string    `json:"last_apply"`
	IntervalHours int       `json:"interval_hours"`
	AutoUpdate    bool      `json:"auto_update"`
	Services      []Service `json:"services"`
	Detail        string    `json:"detail"`
	Configured    bool      `json:"configured"`
	ImagePrefix   string    `json:"image_prefix"`
	VerifyEnabled bool      `json:"verify_enabled"`
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

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	if !c.Enabled() {
		return fmt.Errorf("updater not configured")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Endpoint, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("updater: %s", msg)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Status fetches updater status.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var out Status
	if err := c.do(ctx, http.MethodGet, "/v1/status", nil, &out); err != nil {
		return nil, err
	}
	out.Configured = true
	if out.IntervalHours <= 0 {
		out.IntervalHours = 24
	}
	return &out, nil
}

// Check triggers an image check.
func (c *Client) Check(ctx context.Context) (*Status, error) {
	var out Status
	if err := c.do(ctx, http.MethodPost, "/v1/check", nil, &out); err != nil {
		return nil, err
	}
	out.Configured = true
	return &out, nil
}

// Apply applies pulled images with compose up.
func (c *Client) Apply(ctx context.Context) (*Status, error) {
	var out Status
	if err := c.do(ctx, http.MethodPost, "/v1/apply", nil, &out); err != nil {
		return nil, err
	}
	out.Configured = true
	return &out, nil
}

// SaveSettings stores updater settings.
func (c *Client) SaveSettings(ctx context.Context, settings Settings) error {
	return c.do(ctx, http.MethodPut, "/v1/settings", settings, &settings)
}
