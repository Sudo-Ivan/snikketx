package csrf

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
)

const FieldName = "csrf_token"

func Ensure(sess session.Data) string {
	if t := sess[session.KeyCSRF]; t != "" {
		return t
	}
	b := make([]byte, 16)
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
	return got != "" && got == expected
}
