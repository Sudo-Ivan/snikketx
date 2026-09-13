package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// CookieName is the browser cookie that carries the encrypted session.
	// #nosec G101 -- cookie name, not a credential value
	CookieName = "snikket_session"

	// #nosec G101 -- session map keys, not credential values
	KeyToken = "prosody_access_token"
	// #nosec G101 -- session map keys, not credential values
	KeyScope  = "prosody_scope_cache"
	KeyJID    = "prosody_jid"
	KeyInvite = "invite-session-jid"
	KeyFlashM = "_flash_m"
	KeyFlashC = "_flash_c"
	KeyCSRF   = "_csrf"
	// #nosec G101 -- session map keys, not credential values
	// The pending keys hold a password grant token while the account still
	// owes a second factor. A session carrying only pending keys does not
	// count as signed in: HasSession only sees KeyToken.
	KeyPendingToken = "pending_token"
	KeyPendingScope = "pending_scope"
	KeyPendingJID   = "pending_jid"
	KeyPendingAt    = "pending_at"
	// KeyWebAuthn carries the JSON encoded ceremony session between a
	// WebAuthn begin and finish call.
	KeyWebAuthn = "_webauthn"
	// KeyTOTPSetup carries the base32 TOTP secret while enrollment waits for
	// the first valid code.
	KeyTOTPSetup = "_totp_setup"

	// KeyOIDC marks a session that was established through the external
	// identity provider instead of the Prosody password grant. Such a
	// session carries no Prosody bearer token.
	KeyOIDC = "oidc"
	// The OIDC flow keys hold the transient state of a running
	// authorization code round trip: the anti-forgery state, the ID token
	// nonce, the PKCE verifier and the issue timestamp.
	KeyOIDCState    = "_oidc_state"
	KeyOIDCNonce    = "_oidc_nonce"
	KeyOIDCVerifier = "_oidc_verifier"
	KeyOIDCAt       = "_oidc_at"

	cookieMaxAge  = 12 * time.Hour
	cookieVersion = 1
)

var (
	errShortCookie = errors.New("short cookie")
	errBadCookie   = errors.New("bad cookie")
	encodePool     = sync.Pool{New: func() any { return make([]byte, 0, 512) }}
)

type Store struct {
	aead   cipher.AEAD
	secure bool
	maxAge time.Duration
}

type Data map[string]string

type Flash struct {
	Message  string
	Category string
}

func New(secret []byte, secure bool) (*Store, error) {
	if len(secret) < 32 {
		return nil, errors.New("session secret must be at least 32 bytes")
	}
	sum := sha256.Sum256(secret)
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{aead: aead, secure: secure, maxAge: cookieMaxAge}, nil
}

func (s *Store) Get(r *http.Request) Data {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return Data{}
	}
	raw, err := s.decode(c.Value)
	if err != nil {
		return Data{}
	}
	var d Data
	if err := json.Unmarshal(raw, &d); err != nil {
		return Data{}
	}
	if d == nil {
		return Data{}
	}
	return d
}

func (s *Store) Save(w http.ResponseWriter, d Data) error {
	if d == nil {
		d = Data{}
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	val, err := s.encode(raw)
	if err != nil {
		return err
	}
	// Secure follows the store flag so local HTTP development can opt out.
	// HttpOnly and SameSite are always set.
	// #nosec G124 -- Secure is config-driven for local HTTP; HttpOnly and SameSite are always set
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.maxAge.Seconds()),
	})
	return nil
}

func (s *Store) Clear(w http.ResponseWriter) {
	// #nosec G124 -- Secure matches Save; clearing must use the same cookie attributes
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (d Data) Token() string { return d[KeyToken] }
func (d Data) Scope() string { return d[KeyScope] }
func (d Data) JID() string   { return d[KeyJID] }

// IsOIDC reports whether the session was established through the external
// identity provider. OIDC sessions carry a JID but no Prosody bearer token,
// so callers must not pass Token() to the backend for them.
func (d Data) IsOIDC() bool { return d[KeyOIDC] != "" }

// HasSession reports whether the caller is signed in, either through a
// Prosody token or through the identity provider.
func (d Data) HasSession() bool { return d.Token() != "" || d.IsOIDC() }

func (d Data) IsAdmin() bool {
	scope := d.Scope()
	for len(scope) > 0 {
		var part string
		if i := strings.IndexByte(scope, ' '); i >= 0 {
			part, scope = scope[:i], scope[i+1:]
		} else {
			part, scope = scope, ""
		}
		if part == "prosody:admin" {
			return true
		}
	}
	return false
}

func (d Data) SetAuth(token, scope, jid string) {
	delete(d, KeyOIDC)
	d[KeyToken] = token
	d[KeyScope] = scope
	d[KeyJID] = jid
}

// SetOIDCAuth marks the session as established through the external identity
// provider. No Prosody token is stored: there is no password to trade for one.
func (d Data) SetOIDCAuth(jid string) {
	delete(d, KeyToken)
	delete(d, KeyScope)
	d[KeyJID] = jid
	d[KeyOIDC] = "1"
}

func (d Data) ClearAuth() {
	delete(d, KeyToken)
	delete(d, KeyScope)
	delete(d, KeyJID)
	delete(d, KeyOIDC)
}

// SetPendingAuth stores a freshly issued token under the pending keys while
// the account still has to prove a second factor.
func (d Data) SetPendingAuth(token, scope, jid string, at time.Time) {
	d[KeyPendingToken] = token
	d[KeyPendingScope] = scope
	d[KeyPendingJID] = jid
	d[KeyPendingAt] = strconv.FormatInt(at.Unix(), 10)
}

// PendingJID returns the account address waiting on a second factor.
func (d Data) PendingJID() string { return d[KeyPendingJID] }

// HasPending reports whether a second-factor step is outstanding.
func (d Data) HasPending() bool { return d[KeyPendingToken] != "" }

// PendingAge returns how long ago the pending token was issued. A missing or
// corrupt timestamp reports a negative duration.
func (d Data) PendingAge() time.Duration {
	stamp, err := strconv.ParseInt(d[KeyPendingAt], 10, 64)
	if err != nil || stamp <= 0 {
		return -1
	}
	return time.Since(time.Unix(stamp, 0))
}

// PromotePending moves the pending token into the auth keys. It reports
// whether a pending token existed.
func (d Data) PromotePending() bool {
	token, ok := d[KeyPendingToken]
	if !ok || token == "" {
		return false
	}
	d.SetAuth(token, d[KeyPendingScope], d[KeyPendingJID])
	d.ClearPending()
	return true
}

// ClearPending drops the pending token and its bookkeeping.
func (d Data) ClearPending() {
	delete(d, KeyPendingToken)
	delete(d, KeyPendingScope)
	delete(d, KeyPendingJID)
	delete(d, KeyPendingAt)
}

// RotateAuthSurface drops everything derived from the previous auth state:
// CSRF token, invite state, flashes, pending second-factor state and any
// in-flight WebAuthn ceremony. Callers set fresh values afterwards.
func (d Data) RotateAuthSurface() {
	delete(d, KeyCSRF)
	delete(d, KeyInvite)
	delete(d, KeyFlashM)
	delete(d, KeyFlashC)
	delete(d, KeyWebAuthn)
	delete(d, KeyTOTPSetup)
	delete(d, KeyOIDCState)
	delete(d, KeyOIDCNonce)
	delete(d, KeyOIDCVerifier)
	delete(d, KeyOIDCAt)
	d.ClearPending()
}

// ClearOIDCFlow drops the transient state of a running single sign-on
// round trip.
func (d Data) ClearOIDCFlow() {
	delete(d, KeyOIDCState)
	delete(d, KeyOIDCNonce)
	delete(d, KeyOIDCVerifier)
	delete(d, KeyOIDCAt)
}

func (d Data) PushFlash(msg, category string) {
	d[KeyFlashM] = msg
	d[KeyFlashC] = category
}

func (d Data) PopFlash() *Flash {
	msg, ok := d[KeyFlashM]
	if !ok || msg == "" {
		return nil
	}
	cat := d[KeyFlashC]
	delete(d, KeyFlashM)
	delete(d, KeyFlashC)
	return &Flash{Message: msg, Category: cat}
}

func (s *Store) encode(payload []byte) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	buf := encodePool.Get().([]byte)
	buf = buf[:0]
	buf = append(buf, byte(cookieVersion))
	var stamp [8]byte
	nowUnix := time.Now().Unix()
	if nowUnix < 0 {
		nowUnix = 0
	}
	// #nosec G115 -- Unix time is non-negative here and fits uint64 for cookie stamps
	binary.BigEndian.PutUint64(stamp[:], uint64(nowUnix))
	buf = append(buf, stamp[:]...)
	buf = append(buf, nonce...)
	buf = s.aead.Seal(buf, nonce, payload, buf[:1+8])
	out := base64.RawURLEncoding.EncodeToString(buf)
	encodePool.Put(buf[:0])
	return out, nil
}

func (s *Store) decode(val string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(val)
	if err != nil {
		return nil, err
	}
	ns := s.aead.NonceSize()
	if len(raw) < 1+8+ns+s.aead.Overhead() {
		return nil, errShortCookie
	}
	if raw[0] != cookieVersion {
		return nil, errBadCookie
	}
	issuedU := binary.BigEndian.Uint64(raw[1:9])
	if issuedU == 0 || issuedU > 1<<62 {
		return nil, errBadCookie
	}
	issued := int64(issuedU) // #nosec G115 -- bounded above to stay inside int64
	age := time.Since(time.Unix(issued, 0))
	if age < 0 || age > s.maxAge+time.Minute {
		return nil, errBadCookie
	}
	nonce := raw[9 : 9+ns]
	ciphertext := raw[9+ns:]
	plain, err := s.aead.Open(nil, nonce, ciphertext, raw[:1+8])
	if err != nil {
		return nil, errBadCookie
	}
	return plain, nil
}
