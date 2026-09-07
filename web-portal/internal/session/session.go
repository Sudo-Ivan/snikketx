package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	CookieName = "snikket_session"
	KeyToken   = "prosody_access_token"
	KeyScope   = "prosody_scope_cache"
	KeyJID     = "prosody_jid"
	KeyInvite  = "invite-session-jid"
	KeyFlash   = "_flash"
	KeyCSRF    = "_csrf"
)

type Store struct {
	secret []byte
	secure bool
}

type Data map[string]string

type Flash struct {
	Message  string `json:"m"`
	Category string `json:"c"`
}

func New(secret []byte, secure bool) *Store {
	return &Store{secret: secret, secure: secure}
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
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
	})
	return nil
}

func (s *Store) Clear(w http.ResponseWriter) {
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

func (d Data) Token() string    { return d[KeyToken] }
func (d Data) Scope() string    { return d[KeyScope] }
func (d Data) JID() string      { return d[KeyJID] }
func (d Data) HasSession() bool { return d.Token() != "" }

func (d Data) IsAdmin() bool {
	for _, s := range strings.Fields(d.Scope()) {
		if s == "prosody:admin" {
			return true
		}
	}
	return false
}

func (d Data) SetAuth(token, scope, jid string) {
	d[KeyToken] = token
	d[KeyScope] = scope
	d[KeyJID] = jid
}

func (d Data) ClearAuth() {
	delete(d, KeyToken)
	delete(d, KeyScope)
	delete(d, KeyJID)
}

func (d Data) PushFlash(msg, category string) {
	payload, _ := json.Marshal(Flash{Message: msg, Category: category})
	d[KeyFlash] = string(payload)
}

func (d Data) PopFlash() *Flash {
	raw, ok := d[KeyFlash]
	if !ok || raw == "" {
		return nil
	}
	delete(d, KeyFlash)
	var f Flash
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil
	}
	return &f
}

func (s *Store) encode(payload []byte) (string, error) {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payload)
	sig := mac.Sum(nil)
	out := append(sig, payload...)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func (s *Store) decode(val string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(val)
	if err != nil {
		return nil, err
	}
	if len(raw) < sha256.Size {
		return nil, errors.New("short cookie")
	}
	sig, payload := raw[:sha256.Size], raw[sha256.Size:]
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errors.New("bad signature")
	}
	return payload, nil
}
