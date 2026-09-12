package handlers

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// xmppStub serves a fake Prosody HTTP port and records the requests it saw.
type xmppStub struct {
	mu   sync.Mutex
	seen []stubbedRequest
}

type stubbedRequest struct {
	method   string
	path     string
	query    string
	xff      string
	xfh      string
	xfp      string
	body     string
	upgraded bool
}

func (s *xmppStub) record(r *http.Request, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, stubbedRequest{
		method: r.Method,
		path:   r.URL.Path,
		query:  r.URL.RawQuery,
		xff:    r.Header.Get("X-Forwarded-For"),
		xfh:    r.Header.Get("X-Forwarded-Host"),
		xfp:    r.Header.Get("X-Forwarded-Proto"),
		body:   body,
	})
}

func (s *xmppStub) requests() []stubbedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]stubbedRequest(nil), s.seen...)
}

// stubProsodyXMPP returns a test backend exposing the XMPP protocol endpoints
// plus a private endpoint that must never be reachable through the portal.
func stubProsodyXMPP(t *testing.T) (*httptest.Server, *xmppStub) {
	t.Helper()
	stub := &xmppStub{}
	mux := http.NewServeMux()

	echo := func(marker string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var body []byte
			if r.Body != nil {
				body, _ = io.ReadAll(r.Body)
			}
			stub.record(r, string(body))
			w.Header().Set("X-Stubs", "1")
			_, _ = w.Write([]byte(marker))
		}
	}

	mux.HandleFunc("/xmpp-websocket", func(w http.ResponseWriter, r *http.Request) {
		if !headerHasToken(r.Header, "Connection", "upgrade") ||
			!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			echo("ws-http")(w, r)
			return
		}
		stub.mu.Lock()
		stub.seen = append(stub.seen, stubbedRequest{
			method:   r.Method,
			path:     r.URL.Path,
			xff:      r.Header.Get("X-Forwarded-For"),
			upgraded: true,
		})
		stub.mu.Unlock()

		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		_, _ = conn.Write([]byte("WS-UPSTREAM-DATA"))
	})

	mux.HandleFunc("/http-bind", echo("bosh-response"))
	mux.HandleFunc("/http-bind/", echo("bosh-sub-response"))
	mux.HandleFunc("/.well-known/host-meta", echo("host-meta-xml"))
	mux.HandleFunc("/.well-known/host-meta.json", echo("host-meta-json"))

	// A private endpoint that exists on Prosody but must never be proxied.
	mux.HandleFunc("/admin_api/users", echo("admin-secret"))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, stub
}

func headerHasToken(h http.Header, name, token string) bool {
	for _, value := range h[http.CanonicalHeaderKey(name)] {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// TestXMPPProxyForwardsAnonymousRequests checks the protocol endpoints are
// relayed to Prosody without a portal session and that forwarding headers
// are populated.
func TestXMPPProxyForwardsAnonymousRequests(t *testing.T) {
	backend, stub := stubProsodyXMPP(t)
	app := newTestApp(t, backend.URL)
	portal := httptest.NewServer(app.Routes())
	t.Cleanup(portal.Close)

	portalHost := strings.TrimPrefix(portal.URL, "http://")

	cases := []struct {
		method string
		path   string
		body   string
		want   string
	}{
		{http.MethodGet, "/.well-known/host-meta", "", "host-meta-xml"},
		{http.MethodGet, "/.well-known/host-meta.json", "", "host-meta-json"},
		{http.MethodGet, "/xmpp-websocket", "", "ws-http"},
		{http.MethodPost, "/http-bind", "<body rid='1'/>", "bosh-response"},
		{http.MethodPost, "/http-bind/", "<body rid='2'/>", "bosh-sub-response"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var reader io.Reader
			if tc.body != "" {
				reader = strings.NewReader(tc.body)
			}
			req, err := http.NewRequest(tc.method, portal.URL+tc.path+"?probe=1", reader)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := portal.Client().Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			data, _ := io.ReadAll(resp.Body)

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d, body %s", resp.StatusCode, data)
			}
			if string(data) != tc.want {
				t.Fatalf("body %q, want %q", data, tc.want)
			}
		})
	}

	seen := stub.requests()
	if len(seen) != len(cases) {
		t.Fatalf("backend saw %d requests, want %d", len(seen), len(cases))
	}
	for i, req := range seen {
		if req.method != cases[i].method || req.path != cases[i].path {
			t.Fatalf("request %d: got %s %s, want %s %s", i, req.method, req.path, cases[i].method, cases[i].path)
		}
		if req.query != "probe=1" {
			t.Fatalf("request %d: query %q not preserved", i, req.query)
		}
		if req.xff == "" {
			t.Fatalf("request %d: X-Forwarded-For missing", i)
		}
		if req.xfh != portalHost {
			t.Fatalf("request %d: X-Forwarded-Host %q, want %q", i, req.xfh, portalHost)
		}
	}
	if seen[3].body != "<body rid='1'/>" {
		t.Fatalf("BOSH body not relayed: %q", seen[3].body)
	}
}

// TestXMPPProxyPreservesForwardedProto checks the edge supplied scheme wins
// over the plain HTTP scheme the portal itself sees.
func TestXMPPProxyPreservesForwardedProto(t *testing.T) {
	backend, stub := stubProsodyXMPP(t)
	app := newTestApp(t, backend.URL)
	portal := httptest.NewServer(app.Routes())
	t.Cleanup(portal.Close)

	req, err := http.NewRequest(http.MethodGet, portal.URL+"/.well-known/host-meta", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := portal.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	seen := stub.requests()
	if len(seen) != 1 {
		t.Fatalf("backend saw %d requests", len(seen))
	}
	if seen[0].xfp != "https" {
		t.Fatalf("X-Forwarded-Proto %q, want https", seen[0].xfp)
	}
}

// TestXMPPProxyDoesNotExposeOtherProsodyPaths checks the allowlist keeps the
// rest of the internal HTTP port unreachable.
func TestXMPPProxyDoesNotExposeOtherProsodyPaths(t *testing.T) {
	backend, stub := stubProsodyXMPP(t)
	app := newTestApp(t, backend.URL)
	portal := httptest.NewServer(app.Routes())
	t.Cleanup(portal.Close)

	for _, path := range []string{"/admin_api/users", "/upload", "/metrics-proxy", "/http-bindx"} {
		resp, err := portal.Client().Get(portal.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(data) == "admin-secret" {
			t.Fatalf("%s reached the private Prosody endpoint", path)
		}
	}

	for _, req := range stub.requests() {
		if strings.HasPrefix(req.path, "/admin_api") {
			t.Fatalf("private endpoint received a proxied request")
		}
	}
}

// TestXMPPProxyWebSocketUpgrade performs a real upgrade handshake through the
// full middleware chain to prove the connection can be hijacked end to end.
func TestXMPPProxyWebSocketUpgrade(t *testing.T) {
	backend, stub := stubProsodyXMPP(t)
	app := newTestApp(t, backend.URL)
	portal := httptest.NewServer(app.Routes())
	t.Cleanup(portal.Close)

	u, err := url.Parse(portal.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	_, err = fmt.Fprintf(conn, "GET /xmpp-websocket HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n", u.Host)
	if err != nil {
		t.Fatal(err)
	}

	data, err := io.ReadAll(bufio.NewReader(conn))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	response := string(data)
	if !strings.Contains(response, "101") {
		t.Fatalf("expected 101 upgrade response, got:\n%s", response)
	}
	if !strings.Contains(response, "WS-UPSTREAM-DATA") {
		t.Fatalf("upstream data missing after upgrade, got:\n%s", response)
	}

	for _, req := range stub.requests() {
		if req.path == "/xmpp-websocket" && !req.upgraded {
			t.Fatalf("backend received websocket request without upgrade headers")
		}
	}
}
