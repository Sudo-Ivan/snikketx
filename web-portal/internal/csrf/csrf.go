package csrf

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"

	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
)

const FieldName = "csrf_token"

func Ensure(sess session.Data) string {
	if t := sess[session.KeyCSRF]; t != "" {
		return t
	}
	return Rotate(sess)
}

func Rotate(sess session.Data) string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	t := hex.EncodeToString(b)
	sess[session.KeyCSRF] = t
	return t
}

func Valid(r *http.Request, sess session.Data) bool {
	expected := sess[session.KeyCSRF]
	if expected == "" {
		return false
	}
	got := r.FormValue(FieldName)
	if got == "" {
		got = r.Header.Get("X-CSRF-Token")
	}
	if got == "" {
		return false
	}
	if len(got) != len(expected) {
		_, _ = subtle.ConstantTimeCompare([]byte(expected), []byte(expected)), false
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}
