package csrf_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/quick"

	"github.com/sudo-ivan/snikketx/web-portal/internal/csrf"
	"github.com/sudo-ivan/snikketx/web-portal/internal/session"
)

func TestRotateChangesToken(t *testing.T) {
	data := session.Data{}
	a := csrf.Rotate(data)
	b := csrf.Rotate(data)
	if a == "" || a == b {
		t.Fatalf("rotate failed: %q %q", a, b)
	}
}

func TestValidRejectsEmpty(t *testing.T) {
	data := session.Data{}
	csrf.Rotate(data)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	if csrf.Valid(req, data) {
		t.Fatal("empty should fail")
	}
}

func TestPropertyWrongToken(t *testing.T) {
	f := func(wrong string) bool {
		data := session.Data{}
		tok := csrf.Rotate(data)
		if wrong == tok {
			return true
		}
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(csrf.FieldName+"="+wrong))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return !csrf.Valid(req, data)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}

func FuzzCSRFValid(f *testing.F) {
	data := session.Data{}
	tok := csrf.Rotate(data)
	f.Add(tok)
	f.Add("")
	f.Add("x")
	f.Fuzz(func(t *testing.T, got string) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(csrf.FieldName+"="+got))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		ok := csrf.Valid(req, data)
		if got == tok && !ok {
			t.Fatal("exact token rejected")
		}
		if got != tok && ok {
			t.Fatal("wrong token accepted")
		}
	})
}
