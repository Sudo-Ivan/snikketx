package xmpp_test

import (
	"strings"
	"testing"
	"testing/quick"

	"github.com/sudo-ivan/snikketx/web-portal/internal/xmpp"
)

func TestSplitJID(t *testing.T) {
	local, domain, resource := xmpp.SplitJID("alice@example.test/phone")
	if local != "alice" || domain != "example.test" || resource != "phone" {
		t.Fatalf("%q %q %q", local, domain, resource)
	}
}

func TestPasswordChangeEscapes(t *testing.T) {
	iq := xmpp.PasswordChangeIQ(`a"b@c`, `a"b`, `p&ss<>`)
	if strings.Contains(iq, `a"b`) || strings.Contains(iq, `p&ss<>`) {
		t.Fatalf("unescaped payload: %s", iq)
	}
	if !strings.Contains(iq, "&#34;") || !strings.Contains(iq, "&amp;") {
		t.Fatalf("expected escapes: %s", iq)
	}
}

func TestPropertySplitJoinDomain(t *testing.T) {
	f := func(local, domain string) bool {
		if strings.ContainsAny(local, "@/") || strings.ContainsAny(domain, "@/") || local == "" || domain == "" {
			return true
		}
		l, d, r := xmpp.SplitJID(local + "@" + domain)
		return l == local && d == domain && r == ""
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}

func FuzzExtractIQReply(f *testing.F) {
	f.Add([]byte(`<iq type="result" id="1"/>`))
	f.Add([]byte(`<iq type="error" id="1"><error><item-not-found xmlns="urn:ietf:params:xml:ns:xmpp-stanzas"/></error></iq>`))
	f.Add([]byte(`not xml`))
	f.Fuzz(func(t *testing.T, body []byte) {
		_, _ = xmpp.ExtractIQReply(body)
	})
}
