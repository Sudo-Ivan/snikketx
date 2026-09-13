// Package bots is a client for the mod_snikketx_bots management API. The
// portal authenticates with a static admin token and scopes every call to
// the signed in owner, so users only ever see and manage their own bots.
package bots

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	defaultTimeout = 15 * time.Second
	maxBody        = 1 << 20
	maxBotName     = 48
)

// Bot is the public record returned by the management API.
type Bot struct {
	Name              string `json:"name"`
	JID               string `json:"jid"`
	Owner             string `json:"owner"`
	Label             string `json:"label,omitempty"`
	Disabled          bool   `json:"disabled"`
	Created           int64  `json:"created"`
	WebhookConfigured bool   `json:"webhook_configured"`
	// Token is only populated on create and on token mint. It is a
	// secret shown to the owner exactly once.
	Token string `json:"token,omitempty"`
}

// Token is the metadata of a scoped sxb_ bot token. The token value
// itself is never returned after minting.
type Token struct {
	ID       string   `json:"id"`
	Scopes   []string `json:"scopes"`
	Created  int64    `json:"created"`
	Expires  int64    `json:"expires"`
	LastUsed int64    `json:"last_used"`
}

// APIError is the structured error the module returns.
type APIError struct {
	Code int
	Err  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bots: %s (%d)", e.Err, e.Code)
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// ValidName mirrors the server side bot name rules.
func ValidName(name string) bool {
	return nameRE.MatchString(name) && len(name) <= maxBotName && name != "audit"
}

// Client calls the management API with a static admin token.
type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

// New returns a client for endpoint (for example
// http://server:5280/bots) using the admin bearer token.
func New(endpoint, token string) *Client {
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		token:    token,
		http:     &http.Client{Timeout: defaultTimeout},
	}
}

func (c *Client) call(ctx context.Context, method, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var env struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(raw, &env); err == nil && env.Error != "" {
			return &APIError{Code: resp.StatusCode, Err: env.Error}
		}
		return &APIError{Code: resp.StatusCode, Err: resp.Status}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// List returns the bots owned by jid. The admin token can see every bot,
// so the filter is applied here to keep the API surface per user.
func (c *Client) List(ctx context.Context, owner string) ([]Bot, error) {
	var reply struct {
		Bots []Bot `json:"bots"`
	}
	if err := c.call(ctx, http.MethodGet, "/", nil, &reply); err != nil {
		return nil, err
	}
	out := make([]Bot, 0, len(reply.Bots))
	for _, b := range reply.Bots {
		if b.Owner == owner {
			out = append(out, b)
		}
	}
	return out, nil
}

// Create registers a new bot for owner and returns the record including
// the initial scoped token.
func (c *Client) Create(ctx context.Context, name, owner, label string) (*Bot, error) {
	if !ValidName(name) {
		return nil, &APIError{Code: http.StatusBadRequest, Err: "invalid-name"}
	}
	payload := map[string]any{"name": name, "owner": owner}
	if label != "" {
		payload["label"] = label
	}
	var bot Bot
	if err := c.call(ctx, http.MethodPost, "/", payload, &bot); err != nil {
		return nil, err
	}
	return &bot, nil
}

// Delete removes the bot and drops it from every room.
func (c *Client) Delete(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodDelete, "/"+url.PathEscape(name), nil, nil)
}

// SetDisabled enables or disables the bot.
func (c *Client) SetDisabled(ctx context.Context, name string, disabled bool) error {
	return c.call(ctx, http.MethodPost, "/"+url.PathEscape(name),
		map[string]any{"disabled": disabled}, nil)
}

// SetLabel updates the display label of the bot.
func (c *Client) SetLabel(ctx context.Context, name, label string) error {
	return c.call(ctx, http.MethodPost, "/"+url.PathEscape(name),
		map[string]any{"label": label}, nil)
}

// MintToken creates a new scoped token for the bot. scopes may be nil to
// use the server defaults.
func (c *Client) MintToken(ctx context.Context, bot, tokenName string, scopes []string, ttl int64) (token, id string, err error) {
	payload := map[string]any{}
	if tokenName != "" {
		payload["name"] = tokenName
	}
	if len(scopes) > 0 {
		payload["scopes"] = scopes
	}
	if ttl > 0 {
		payload["ttl"] = ttl
	}
	var reply struct {
		Token string `json:"token"`
		ID    string `json:"id"`
	}
	if err := c.call(ctx, http.MethodPost,
		"/"+url.PathEscape(bot)+"/tokens", payload, &reply); err != nil {
		return "", "", err
	}
	return reply.Token, reply.ID, nil
}

// ListTokens returns the token metadata of the bot.
func (c *Client) ListTokens(ctx context.Context, bot string) ([]Token, error) {
	var reply struct {
		Tokens []Token `json:"tokens"`
	}
	if err := c.call(ctx, http.MethodGet,
		"/"+url.PathEscape(bot)+"/tokens", nil, &reply); err != nil {
		return nil, err
	}
	return reply.Tokens, nil
}

// RevokeToken deletes one token of the bot.
func (c *Client) RevokeToken(ctx context.Context, bot, tokenID string) error {
	return c.call(ctx, http.MethodDelete,
		"/"+url.PathEscape(bot)+"/tokens/"+url.PathEscape(tokenID), nil, nil)
}
