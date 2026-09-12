// Package credstore persists the per-account second-factor state of the web
// portal: registered WebAuthn credentials and TOTP secrets. The data lives in
// a single JSON document inside the state directory, written atomically, and
// TOTP secrets are sealed with an AES-GCM key derived from the portal secret
// key so they are not stored in plain text.
package credstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	// fileName is the document inside the state directory holding every
	// account's second-factor state.
	fileName = "credentials.json" // #nosec G101 -- state filename, not a credential value
	// fileVersion is the on-disk schema version.
	fileVersion = 1
	// userIDBytes is the size of the random WebAuthn user handle issued to an
	// account. The specification recommends a random 64 byte handle.
	userIDBytes = 64
	// maxPasskeys bounds the credentials one account may register.
	maxPasskeys = 10
)

// ErrTooManyPasskeys is returned when an account already holds the maximum
// number of registered passkeys.
var ErrTooManyPasskeys = errors.New("credstore: passkey limit reached")

// Passkey is one registered WebAuthn credential together with the portal-side
// metadata shown in the account settings.
type Passkey struct {
	// ID is the credential id, duplicated from Credential for indexing.
	ID []byte `json:"id"`
	// Name is the friendly label the account owner gave the credential.
	Name string `json:"name,omitempty"`
	// CreatedAt records when the credential was registered.
	CreatedAt time.Time `json:"created_at"`
	// LastUsedAt records the most recent successful assertion.
	LastUsedAt time.Time `json:"last_used_at,omitempty"`
	// Credential is the credential record go-webauthn verifies against. It is
	// stored verbatim so updated sign counters can be written back after
	// every login.
	Credential webauthn.Credential `json:"credential"`
}

// Account is the second-factor state of one portal account, keyed by the bare
// XMPP address.
type Account struct {
	// UserID is the random WebAuthn user handle of the account.
	UserID []byte `json:"user_id,omitempty"`
	// Passkeys lists the registered WebAuthn credentials.
	Passkeys []Passkey `json:"passkeys,omitempty"`
	// TOTPSecret holds the sealed base32 TOTP secret. Empty when TOTP is off.
	TOTPSecret string `json:"totp_secret,omitempty"`
	// TOTPEnabled reports whether a TOTP code is required after password auth.
	TOTPEnabled bool `json:"totp_enabled,omitempty"`
	// TOTPCreatedAt records when the current TOTP secret was activated.
	TOTPCreatedAt time.Time `json:"totp_created_at,omitempty"`
}

// NeedsSecondFactor reports whether the account must complete a second-factor
// step after the password grant succeeds.
func (a *Account) NeedsSecondFactor() bool {
	return a != nil && (a.TOTPEnabled || len(a.Passkeys) > 0)
}

// fileData is the on-disk document.
type fileData struct {
	Version  int                `json:"version"`
	Accounts map[string]Account `json:"accounts"`
}

// Store is a JSON document guarded by a mutex and sealed with a key derived
// from the portal secret key. It is safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	path string
	aead cipher.AEAD
	data fileData
}

// Open loads or creates the credential document at dir/fileName. The secret
// parameter is the portal secret key; TOTP secrets are sealed with a key
// derived from it.
func Open(dir string, secret []byte) (*Store, error) {
	if len(secret) < 32 {
		return nil, errors.New("credstore: secret must be at least 32 bytes")
	}
	sum := sha256.Sum256(append(append([]byte{}, secret...), ":credstore-v1"...))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	store := &Store{
		path: filepath.Join(dir, fileName),
		aead: aead,
		data: fileData{Version: fileVersion, Accounts: map[string]Account{}},
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

// load reads the document when it exists. A missing file is not an error.
func (s *Store) load() error {
	// #nosec G304 -- path is built from the operator-configured state dir
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("credstore: read: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}
	var data fileData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("credstore: decode: %w", err)
	}
	if data.Accounts == nil {
		data.Accounts = map[string]Account{}
	}
	s.data = data
	return nil
}

// save writes the document atomically with private permissions.
func (s *Store) save() error {
	payload, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("credstore: encode: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil && !os.IsExist(err) {
		return fmt.Errorf("credstore: create dir: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("credstore: write: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("credstore: replace: %w", err)
	}
	return nil
}

// seal encrypts and base64 encodes a TOTP secret for storage.
func (s *Store) seal(plain string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := s.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// open decodes and decrypts a sealed TOTP secret.
func (s *Store) open(sealed string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	ns := s.aead.NonceSize()
	if len(raw) < ns+s.aead.Overhead() {
		return "", errors.New("credstore: short secret")
	}
	plain, err := s.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// key normalises an account address to the map key form.
func key(jid string) string {
	return strings.ToLower(strings.TrimSpace(jid))
}

// Get returns a copy of the account's second-factor state, or nil when the
// account has none.
func (s *Store) Get(jid string) *Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	acct, ok := s.data.Accounts[key(jid)]
	if !ok {
		return nil
	}
	out := acct
	out.Passkeys = append([]Passkey(nil), acct.Passkeys...)
	return &out
}

// EnsureUserID returns the account's WebAuthn user handle, creating the
// account record and the handle when needed.
func (s *Store) EnsureUserID(jid string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(jid)
	acct := s.data.Accounts[k]
	if len(acct.UserID) > 0 {
		return append([]byte(nil), acct.UserID...), nil
	}
	handle := make([]byte, userIDBytes)
	if _, err := io.ReadFull(rand.Reader, handle); err != nil {
		return nil, err
	}
	acct.UserID = handle
	s.data.Accounts[k] = acct
	if err := s.save(); err != nil {
		return nil, err
	}
	return append([]byte(nil), handle...), nil
}

// AddPasskey stores a verified credential for the account. Registering the
// same credential id twice replaces the earlier record so a re-registered
// authenticator never duplicates.
func (s *Store) AddPasskey(jid, name string, cred webauthn.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(jid)
	acct := s.data.Accounts[k]
	for i := range acct.Passkeys {
		if string(acct.Passkeys[i].ID) == string(cred.ID) {
			acct.Passkeys[i].Credential = cred
			acct.Passkeys[i].LastUsedAt = time.Now().UTC()
			s.data.Accounts[k] = acct
			return s.save()
		}
	}
	if len(acct.Passkeys) >= maxPasskeys {
		return ErrTooManyPasskeys
	}
	acct.Passkeys = append(acct.Passkeys, Passkey{
		ID:         append([]byte(nil), cred.ID...),
		Name:       strings.TrimSpace(name),
		CreatedAt:  time.Now().UTC(),
		Credential: cred,
	})
	s.data.Accounts[k] = acct
	return s.save()
}

// UpdatePasskey writes back the credential record go-webauthn returned from a
// successful assertion so the cloned-credential sign counter stays current.
func (s *Store) UpdatePasskey(jid string, cred webauthn.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(jid)
	acct, ok := s.data.Accounts[k]
	if !ok {
		return nil
	}
	for i := range acct.Passkeys {
		if string(acct.Passkeys[i].ID) == string(cred.ID) {
			acct.Passkeys[i].Credential = cred
			acct.Passkeys[i].LastUsedAt = time.Now().UTC()
			s.data.Accounts[k] = acct
			return s.save()
		}
	}
	return nil
}

// RemovePasskey deletes one credential of the account. It reports whether the
// credential existed.
func (s *Store) RemovePasskey(jid string, id []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(jid)
	acct, ok := s.data.Accounts[k]
	if !ok {
		return false, nil
	}
	out := acct.Passkeys[:0]
	found := false
	for _, pk := range acct.Passkeys {
		if string(pk.ID) == string(id) {
			found = true
			continue
		}
		out = append(out, pk)
	}
	if !found {
		return false, nil
	}
	acct.Passkeys = out
	s.data.Accounts[k] = acct
	return true, s.save()
}

// SetTOTP activates a TOTP secret for the account. The secret is sealed
// before it reaches the document.
func (s *Store) SetTOTP(jid, secret string) error {
	sealed, err := s.seal(secret)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(jid)
	acct := s.data.Accounts[k]
	acct.TOTPSecret = sealed
	acct.TOTPEnabled = true
	acct.TOTPCreatedAt = time.Now().UTC()
	s.data.Accounts[k] = acct
	return s.save()
}

// ClearTOTP removes the account's TOTP secret.
func (s *Store) ClearTOTP(jid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(jid)
	acct, ok := s.data.Accounts[k]
	if !ok {
		return nil
	}
	acct.TOTPSecret = ""
	acct.TOTPEnabled = false
	acct.TOTPCreatedAt = time.Time{}
	s.data.Accounts[k] = acct
	return s.save()
}

// TOTPSecret returns the unsealed base32 secret when TOTP is enabled for the
// account.
func (s *Store) TOTPSecret(jid string) (string, bool, error) {
	s.mu.Lock()
	sealed := s.data.Accounts[key(jid)].TOTPSecret
	s.mu.Unlock()
	if sealed == "" {
		return "", false, nil
	}
	secret, err := s.open(sealed)
	if err != nil {
		return "", false, err
	}
	return secret, true, nil
}
