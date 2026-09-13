package linkpreview

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// publicIP stands in for any routable address DNS could return.
var publicIP = netip.MustParseAddr("93.184.216.34")

func TestPublicAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1", false},
		{"127.53.0.1", false},
		{"10.0.0.1", false},
		{"10.255.255.255", false},
		{"172.16.0.1", false},
		{"172.31.255.255", false},
		{"172.15.0.1", true},
		{"172.32.0.1", true},
		{"192.168.0.1", false},
		{"192.168.255.255", false},
		{"169.254.1.1", false},
		{"0.0.0.0", false},
		{"0.1.2.3", false},
		{"100.64.0.1", false},
		{"192.0.0.8", false},
		{"192.0.2.1", false},
		{"198.51.100.1", false},
		{"203.0.113.1", false},
		{"224.0.0.1", false},
		{"240.0.0.1", false},
		{"255.255.255.255", false},
		{"::", false},
		{"::1", false},
		{"fc00::1", false},
		{"fd00::1", false},
		{"fe80::1", false},
		{"ff02::1", false},
		{"64:ff9b::8.8.8.8", false},
		{"2001:db8::1", false},
		{"::ffff:127.0.0.1", false},
		{"::ffff:10.0.0.1", false},
		{"::ffff:8.8.8.8", true},
		{"8.8.8.8", true},
		{"93.184.216.34", true},
		{"2606:4700:4700::1111", true},
	}
	for _, tc := range cases {
		addr, err := netip.ParseAddr(tc.addr)
		if err != nil {
			t.Fatalf("ParseAddr(%q): %v", tc.addr, err)
		}
		if got := publicAddr(addr); got != tc.want {
			t.Errorf("publicAddr(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestValidateRequestURL(t *testing.T) {
	bad := []string{
		"",
		"ftp://example.test/x",
		"gopher://example.test/x",
		"file:///etc/passwd",
		"example.test/no-scheme",
		"http:///path",
		"http://example.test:8080/x",
		"http://example.test:22/x",
		"https://example.test:8443/x",
		"http://user:pw@example.test/x",
		"http://user@example.test/x",
		strings.Repeat("a", maxURLLength+1),
	}
	for _, raw := range bad {
		if _, err := validateRequestURL(raw); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("validateRequestURL(%q) = %v, want ErrInvalidURL", raw, err)
		}
	}

	good := []string{
		"http://example.test/x",
		"https://example.test/x",
		"http://example.test:80/x",
		"https://example.test:443/x",
		"http://example.test:443/x",
	}
	for _, raw := range good {
		if _, err := validateRequestURL(raw); err != nil {
			t.Errorf("validateRequestURL(%q) = %v, want nil", raw, err)
		}
	}
}

// newTestFetcher returns a Fetcher whose DNS answers come from names and
// whose connections all land on server, so the full request path including
// redirects can be exercised without real outbound traffic.
func newTestFetcher(t *testing.T, server *httptest.Server, names map[string][]netip.Addr) *Fetcher {
	t.Helper()
	f := New()
	f.resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
		if ip, err := netip.ParseAddr(host); err == nil {
			return []netip.Addr{ip}, nil
		}
		if addrs, ok := names[host]; ok {
			return addrs, nil
		}
		return nil, fmt.Errorf("no such host %q", host)
	}
	f.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}
	return f
}

func publicNames(hosts ...string) map[string][]netip.Addr {
	names := make(map[string][]netip.Addr, len(hosts))
	for _, h := range hosts {
		names[h] = []netip.Addr{publicIP}
	}
	return names
}

func TestFetchMetadataBlockedTargets(t *testing.T) {
	// No request should ever reach the server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("blocked request reached server: %s", r.URL)
	}))
	t.Cleanup(server.Close)

	f := newTestFetcher(t, server, map[string][]netip.Addr{
		// A resolver that interprets the decimal and other non standard
		// IP spellings still ends up validated.
		"2130706433":    {netip.MustParseAddr("127.0.0.1")},
		"0x7f000001":    {netip.MustParseAddr("127.0.0.1")},
		"internal.test": {netip.MustParseAddr("10.1.2.3")},
		"mixed.test":    {publicIP, netip.MustParseAddr("192.168.1.1")},
		"empty.test":    {},
	})

	cases := []struct {
		raw  string
		want error
	}{
		{"http://127.0.0.1/x", ErrBlockedAddress},
		{"http://127.0.0.1.nip.io/x", nil}, // resolves via names map miss below
		{"http://10.0.0.5/x", ErrBlockedAddress},
		{"http://172.16.0.9/x", ErrBlockedAddress},
		{"http://192.168.1.1/x", ErrBlockedAddress},
		{"http://169.254.169.254/latest/meta-data", ErrBlockedAddress},
		{"http://0.0.0.0/x", ErrBlockedAddress},
		{"http://[::1]/x", ErrBlockedAddress},
		{"http://[fc00::1]/x", ErrBlockedAddress},
		{"http://[fe80::1]/x", ErrBlockedAddress},
		{"http://[::ffff:127.0.0.1]/x", ErrBlockedAddress},
		{"http://2130706433/x", ErrBlockedAddress},
		{"http://0x7f000001/x", ErrBlockedAddress},
		{"http://internal.test/x", ErrBlockedAddress},
		{"http://mixed.test/x", ErrBlockedAddress},
		{"http://empty.test/x", ErrNoAddress},
		{"http://127.0.0.1:8080/x", ErrInvalidURL},
		{"ftp://example.test/x", ErrInvalidURL},
	}
	for _, tc := range cases {
		_, err := f.FetchMetadata(context.Background(), tc.raw)
		if tc.want == nil {
			// DNS miss for the wildcard-ish name: any error is fine as long
			// as nothing was served.
			if err == nil {
				t.Errorf("FetchMetadata(%q) unexpectedly succeeded", tc.raw)
			}
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("FetchMetadata(%q) = %v, want %v", tc.raw, err, tc.want)
		}
	}
}

const ogFixture = `<!doctype html><html><head>
<title>Fallback Title</title>
<meta name="description" content="Fallback description">
<meta property="og:title" content="OG Title">
<meta property="og:description" content="OG Description">
<meta property="og:site_name" content="OG Site">
<meta property="og:image" content="/img/pic.png">
<meta property="og:url" content="https://example.test/canonical">
<link rel="icon" href="/icons/site.ico">
</head><body>hello</body></html>`

func TestFetchMetadataParsesOG(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("User-Agent = %q, want %q", r.Header.Get("User-Agent"), userAgent)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(ogFixture))
	}))
	t.Cleanup(server.Close)

	f := newTestFetcher(t, server, publicNames("example.test"))
	meta, err := f.FetchMetadata(context.Background(), "http://example.test/page")
	if err != nil {
		t.Fatal(err)
	}
	if meta.URL != "https://example.test/canonical" {
		t.Errorf("URL = %q", meta.URL)
	}
	if meta.Title != "OG Title" {
		t.Errorf("Title = %q", meta.Title)
	}
	if meta.Description != "OG Description" {
		t.Errorf("Description = %q", meta.Description)
	}
	if meta.SiteName != "OG Site" {
		t.Errorf("SiteName = %q", meta.SiteName)
	}
	if meta.Image != "http://example.test/img/pic.png" {
		t.Errorf("Image = %q", meta.Image)
	}
	if meta.Favicon != "http://example.test/icons/site.ico" {
		t.Errorf("Favicon = %q", meta.Favicon)
	}
}

func TestFetchMetadataFallbacks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Plain Title</title>` +
			`<meta name="description" content="Plain description"></head></html>`))
	}))
	t.Cleanup(server.Close)

	f := newTestFetcher(t, server, publicNames("example.test"))
	meta, err := f.FetchMetadata(context.Background(), "http://example.test/page")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Plain Title" {
		t.Errorf("Title = %q", meta.Title)
	}
	if meta.Description != "Plain description" {
		t.Errorf("Description = %q", meta.Description)
	}
	if meta.URL != "http://example.test/page" {
		t.Errorf("URL = %q", meta.URL)
	}
	if meta.Favicon != "http://example.test/favicon.ico" {
		t.Errorf("Favicon = %q", meta.Favicon)
	}
}

func TestFetchMetadataRejectsNonHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("not a page"))
	}))
	t.Cleanup(server.Close)

	f := newTestFetcher(t, server, publicNames("example.test"))
	if _, err := f.FetchMetadata(context.Background(), "http://example.test/x"); !errors.Is(err, ErrNotHTML) {
		t.Fatalf("err = %v, want ErrNotHTML", err)
	}
}

func TestFetchMetadataUpstreamStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	f := newTestFetcher(t, server, publicNames("example.test"))
	_, err := f.FetchMetadata(context.Background(), "http://example.test/x")
	var upstream *UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != http.StatusNotFound {
		t.Fatalf("err = %v, want UpstreamError 404", err)
	}
}

func TestFetchFollowsAndRevalidatesRedirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://hop.test/middle", http.StatusFound)
	})
	mux.HandleFunc("/middle", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Landed</title>` +
			`<meta property="og:image" content="pic.png"></head></html>`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	f := newTestFetcher(t, server, publicNames("example.test", "hop.test"))
	meta, err := f.FetchMetadata(context.Background(), "http://example.test/start")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title != "Landed" {
		t.Errorf("Title = %q", meta.Title)
	}
	// The relative image resolves against the final URL, not the first hop.
	if meta.Image != "http://hop.test/pic.png" {
		t.Errorf("Image = %q", meta.Image)
	}
}

func TestFetchRejectsRedirectToPrivate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "http://internal.test/secret", http.StatusFound)
			return
		}
		t.Errorf("redirect to private target was followed: %s", r.URL)
	}))
	t.Cleanup(server.Close)

	f := newTestFetcher(t, server, map[string][]netip.Addr{
		"example.test":  {publicIP},
		"internal.test": {netip.MustParseAddr("127.0.0.1")},
	})
	if _, err := f.FetchMetadata(context.Background(), "http://example.test/start"); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("err = %v, want ErrBlockedAddress", err)
	}
}

func TestFetchRejectsRedirectSchemeDowngrade(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "ftp://example.test/x", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	f := newTestFetcher(t, server, publicNames("example.test"))
	if _, err := f.FetchMetadata(context.Background(), "http://example.test/start"); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("err = %v, want ErrInvalidURL", err)
	}
}

func TestFetchTooManyRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	f := newTestFetcher(t, server, publicNames("example.test"))
	if _, err := f.FetchMetadata(context.Background(), "http://example.test/"); !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("err = %v, want ErrTooManyRedirects", err)
	}
}

func TestFetchImage(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n")
	mux := http.NewServeMux()
	mux.HandleFunc("/pic.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	})
	mux.HandleFunc("/not-image", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	})
	mux.HandleFunc("/huge.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(make([]byte, maxImageBody+10))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	f := newTestFetcher(t, server, publicNames("example.test"))

	data, ct, err := f.FetchImage(context.Background(), "http://example.test/pic.png")
	if err != nil {
		t.Fatal(err)
	}
	if ct != "image/png" {
		t.Errorf("contentType = %q", ct)
	}
	if string(data) != string(png) {
		t.Errorf("data mismatch, %d bytes", len(data))
	}

	if _, _, err := f.FetchImage(context.Background(), "http://example.test/not-image"); !errors.Is(err, ErrNotImage) {
		t.Fatalf("err = %v, want ErrNotImage", err)
	}
	if _, _, err := f.FetchImage(context.Background(), "http://example.test/huge.png"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestFetchMetadataCaches(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Cached</title></head></html>`))
	}))
	t.Cleanup(server.Close)

	f := newTestFetcher(t, server, publicNames("example.test"))
	for i := 0; i < 3; i++ {
		if _, err := f.FetchMetadata(context.Background(), "http://example.test/x"); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Fatalf("server hit %d times, want 1", hits)
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(2)
	if ok, _ := l.Allow("alice@example.test"); !ok {
		t.Fatal("first request denied")
	}
	if ok, _ := l.Allow("alice@example.test"); !ok {
		t.Fatal("second request denied")
	}
	if ok, wait := l.Allow("alice@example.test"); ok || wait <= 0 {
		t.Fatal("third request allowed or no retry hint")
	}
	// A different account is unaffected.
	if ok, _ := l.Allow("bob@example.test"); !ok {
		t.Fatal("other account denied")
	}
}
