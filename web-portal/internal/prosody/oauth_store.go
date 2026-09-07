package prosody

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// oauthCredentialsFile is the on-disk OAuth client registration used across
// portal restarts so existing browser sessions keep working.
const oauthCredentialsFile = "oauth_client.json"

type storedOAuthClient struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// SetCredentialsPath configures where dynamic client registration is saved.
// An empty path disables persistence.
func (c *Client) SetCredentialsPath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.credentialsPath = strings.TrimSpace(path)
}

// LoadStoredCredentials reads previously registered OAuth client credentials
// from disk when present. Missing files are ignored.
func (c *Client) LoadStoredCredentials() error {
	c.mu.Lock()
	path := c.credentialsPath
	c.mu.Unlock()
	if path == "" {
		return nil
	}

	// #nosec G304 -- path is operator-configured absolute state path
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("prosody: read oauth credentials: %w", err)
	}

	var stored storedOAuthClient
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("prosody: decode oauth credentials: %w", err)
	}
	stored.ClientID = strings.TrimSpace(stored.ClientID)
	stored.ClientSecret = strings.TrimSpace(stored.ClientSecret)
	if stored.ClientID == "" || stored.ClientSecret == "" {
		return nil
	}

	c.mu.Lock()
	c.clientID = stored.ClientID
	c.clientSecret = stored.ClientSecret
	c.mu.Unlock()
	return nil
}

func (c *Client) saveStoredCredentials(clientID, clientSecret string) error {
	c.mu.Lock()
	path := c.credentialsPath
	c.mu.Unlock()
	if path == "" {
		return nil
	}

	payload, err := json.Marshal(storedOAuthClient{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	})
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil && !os.IsExist(err) {
		return fmt.Errorf("prosody: create oauth credentials dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("prosody: write oauth credentials: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("prosody: replace oauth credentials: %w", err)
	}
	return nil
}

// DefaultOAuthCredentialsPath returns the state-dir path used for persisted
// OAuth client registration.
func DefaultOAuthCredentialsPath(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		stateDir = "/var/lib/snikket-web-portal"
	}
	return filepath.Join(stateDir, oauthCredentialsFile)
}
