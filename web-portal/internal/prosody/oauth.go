package prosody

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

const (
	// softwareID identifies web-portal.snikket.org to the authorisation
	// server during dynamic client registration.
	softwareID = "22aa246e-4373-51cb-bcaa-9f73bb235b84"
	clientName = "Snikket web portal"
)

// ErrInvalidCredentials is returned when the authorisation server rejects the
// password grant.
var ErrInvalidCredentials = errors.New("prosody: invalid credentials")

// requestedScope is the scope string asked for on every password grant. The
// server narrows it down to what the account is actually entitled to.
func requestedScope() string {
	return strings.Join([]string{ScopeRestricted, ScopeDefault, ScopeAdmin}, " ")
}

// IsClientRegistered reports whether OAuth 2.0 client credentials have already
// been obtained.
func (c *Client) IsClientRegistered() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clientID != "" && c.clientSecret != ""
}

// clientCredentials returns the registered client id and secret.
func (c *Client) clientCredentials() (string, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.clientID == "" || c.clientSecret == "" {
		return "", "", false
	}
	return c.clientID, c.clientSecret, true
}

// RegisterClient performs OAuth 2.0 dynamic client registration against the
// server. The credentials it returns are cached on the client.
func (c *Client) RegisterClient(ctx context.Context) error {
	payload := clientRegistrationRequest{
		ClientName: clientName,
		ClientURI:  "https://" + c.Domain,
		// The redirect URI is never used because the portal uses the
		// password grant. Prosody still requires at least one, so a
		// sensible value is registered up front.
		RedirectURIs:            []string{"https://" + c.Domain + "/login_result"},
		ApplicationType:         "web",
		GrantTypes:              []string{"password"},
		ResponseTypes:           []string{},
		TokenEndpointAuthMethod: "client_secret_post",
		Scope:                   requestedScope(),
		SoftwareID:              softwareID,
		SoftwareVersion:         c.Version,
	}

	resp, err := c.requestJSON(ctx, http.MethodPost, c.registerClientEndpoint(), "", payload)
	if err != nil {
		return err
	}
	defer closeBody(resp)

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return &HTTPError{
			Status:  resp.StatusCode,
			Message: "failed to register with backend server",
			Body:    string(body),
		}
	}

	var registration clientRegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&registration); err != nil {
		return fmt.Errorf("prosody: decode client registration: %w", err)
	}
	if registration.ClientID == "" || registration.ClientSecret == "" {
		return errors.New("prosody: client registration returned no credentials")
	}

	c.mu.Lock()
	c.clientID = registration.ClientID
	c.clientSecret = registration.ClientSecret
	c.mu.Unlock()
	return nil
}

// ensureClientRegistered registers the OAuth 2.0 client unless that has
// already happened.
func (c *Client) ensureClientRegistered(ctx context.Context) error {
	if c.IsClientRegistered() {
		return nil
	}

	c.registerMu.Lock()
	defer c.registerMu.Unlock()

	if c.IsClientRegistered() {
		return nil
	}
	return c.RegisterClient(ctx)
}

// bearerToken runs the OAuth 2.0 password grant, registering the client first
// when that has not happened yet. Only the localpart of the address is sent as
// the username.
func (c *Client) bearerToken(ctx context.Context, address, password string) (*TokenInfo, error) {
	if err := c.ensureClientRegistered(ctx); err != nil {
		return nil, err
	}

	clientID, clientSecret, ok := c.clientCredentials()
	if !ok {
		return nil, errors.New("prosody: client is not registered")
	}

	localpart, _, _ := xmpp.SplitJID(address)
	if localpart == "" {
		localpart = address
	}

	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("username", localpart)
	form.Set("password", password)
	form.Set("scope", requestedScope())

	req, err := c.newRequest(ctx, http.MethodPost, c.loginEndpoint(), "", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		return nil, err
	}

	var auth tokenResponse
	decodeErr := json.Unmarshal(body, &auth)

	switch resp.StatusCode {
	case http.StatusOK:
		if decodeErr != nil {
			return nil, fmt.Errorf("prosody: decode token response: %w", decodeErr)
		}
		if !strings.EqualFold(auth.TokenType, "bearer") {
			return nil, fmt.Errorf("prosody: unsupported token type %q", auth.TokenType)
		}
		return &TokenInfo{
			Token:  auth.AccessToken,
			Scopes: strings.Fields(auth.Scope),
		}, nil
	case http.StatusBadRequest, http.StatusUnauthorized:
		if decodeErr == nil && auth.Error == "invalid_grant" {
			return nil, ErrInvalidCredentials
		}
		return nil, &HTTPError{
			Status:  resp.StatusCode,
			Message: auth.Error,
			Body:    string(body),
		}
	default:
		return nil, &HTTPError{
			Status:  resp.StatusCode,
			Message: "unexpected authentication reply",
			Body:    string(body),
		}
	}
}

// Login exchanges an address and password for a bearer token.
func (c *Client) Login(ctx context.Context, address, password string) (*TokenInfo, error) {
	return c.bearerToken(ctx, address, password)
}

// RevokeToken asks the server to invalidate an access token.
func (c *Client) RevokeToken(ctx context.Context, token string) error {
	form := url.Values{}
	form.Set("token", token)
	form.Set("token_type_hint", "access_token")

	req, err := c.newRequest(ctx, http.MethodPost, c.revokeEndpoint(), "", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer closeBody(resp)

	return errorFromResponse(resp)
}

// Logout revokes the access token. Callers clear their local session state
// regardless of whether revocation succeeded.
func (c *Client) Logout(ctx context.Context, token string) error {
	return c.RevokeToken(ctx, token)
}

// ChangePassword replaces the account password. It deliberately obtains a
// fresh token with the current password instead of reusing the caller's token,
// and returns that token so the caller can replace its session state.
func (c *Client) ChangePassword(ctx context.Context, address, currentPassword, newPassword string) (*TokenInfo, error) {
	tokenInfo, err := c.bearerToken(ctx, address, currentPassword)
	if err != nil {
		return nil, err
	}

	// mod_rest and mod_register only recognise the password change stanza
	// when it is addressed to the bare account JID.
	localpart, domain, _ := xmpp.SplitJID(address)
	bare := localpart + "@" + domain

	if _, err := c.xmlIQCall(ctx, tokenInfo.Token, xmpp.PasswordChangeIQ(bare, localpart, newPassword)); err != nil {
		return nil, err
	}
	return tokenInfo, nil
}
