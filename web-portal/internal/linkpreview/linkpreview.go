// Package linkpreview fetches OpenGraph metadata and images on behalf of
// authenticated clients so they never contact third-party sites directly.
//
// The fetcher is hardened against SSRF: only http and https on ports 80 and
// 443 are allowed, every resolved address must be a public global unicast
// address, redirects are followed manually with the same checks applied at
// each hop, and connections are dialled to the already validated address so a
// DNS rebinding between validation and connect is impossible.
package linkpreview

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultUserHourlyLimit caps preview lookups per authenticated account.
	DefaultUserHourlyLimit = 60

	defaultTimeout = 8 * time.Second
	headerTimeout  = 5 * time.Second
	maxRedirects   = 3
	maxURLLength   = 2048
	maxHTMLBody    = 1536 * 1024
	maxImageBody   = 768 * 1024
	cacheTTL       = 15 * time.Minute
	cacheMax       = 512
	userAgent      = "SnikketX-LinkPreview/1.0"
)

// Errors surfaced to the caller so the handler can pick a status code.
var (
	// ErrInvalidURL is a malformed or disallowed target URL.
	ErrInvalidURL = errors.New("linkpreview: invalid url")
	// ErrBlockedAddress means the target resolves to a non-public address.
	ErrBlockedAddress = errors.New("linkpreview: target resolves to a non-public address")
	// ErrNoAddress means the target host resolved to nothing.
	ErrNoAddress = errors.New("linkpreview: target host has no addresses")
	// ErrTooManyRedirects means the redirect chain exceeded the limit.
	ErrTooManyRedirects = errors.New("linkpreview: too many redirects")
	// ErrNotHTML means the metadata endpoint received a non HTML reply.
	ErrNotHTML = errors.New("linkpreview: response is not html")
	// ErrNotImage means the image endpoint received a non image reply.
	ErrNotImage = errors.New("linkpreview: response is not an image")
	// ErrTooLarge means the image body exceeded the size cap.
	ErrTooLarge = errors.New("linkpreview: response body too large")
)

// UpstreamError reports a non 200 reply from the remote server.
type UpstreamError struct {
	Status int
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("linkpreview: upstream status %d", e.Status)
}

// Fetcher retrieves preview metadata and images through the hardened client.
type Fetcher struct {
	timeout time.Duration

	metaCache  *cache
	imageCache *cache

	// resolve maps a hostname to addresses. It is a field so tests can pin
	// DNS answers without touching the network.
	resolve func(ctx context.Context, host string) ([]netip.Addr, error)
	// dial opens the underlying connection to an already validated
	// ip:port. Tests swap it to reach a local httptest server.
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// New returns a Fetcher with production defaults.
func New() *Fetcher {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return &Fetcher{
		timeout:    defaultTimeout,
		metaCache:  newCache(cacheTTL, cacheMax),
		imageCache: newCache(cacheTTL, cacheMax),
		resolve:    defaultResolve,
		dial:       dialer.DialContext,
	}
}

// Metadata is the OpenGraph summary returned to the client. Empty fields are
// omitted from the JSON document.
type Metadata struct {
	URL         string `json:"url,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	SiteName    string `json:"site_name,omitempty"`
	Image       string `json:"image,omitempty"`
	Favicon     string `json:"favicon,omitempty"`
}

// FetchMetadata retrieves the page at rawURL and extracts its preview data.
// Successful results are cached briefly keyed by the requested URL.
func (f *Fetcher) FetchMetadata(ctx context.Context, rawURL string) (*Metadata, error) {
	if v, ok := f.metaCache.get(rawURL); ok {
		return v.(*Metadata), nil
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	resp, finalURL, err := f.fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, &UpstreamError{Status: resp.StatusCode}
	}
	if !mediaTypeIs(resp.Header.Get("Content-Type"), "text/html") {
		return nil, ErrNotHTML
	}

	meta := parseMetadata(io.LimitReader(resp.Body, maxHTMLBody), finalURL)
	f.metaCache.set(rawURL, meta)
	return meta, nil
}

// FetchImage retrieves an image or favicon at rawURL. Only image/* responses
// are returned and the body is capped at the image size limit.
func (f *Fetcher) FetchImage(ctx context.Context, rawURL string) (data []byte, contentType string, err error) {
	if v, ok := f.imageCache.get(rawURL); ok {
		img := v.(*cachedImage)
		return img.data, img.contentType, nil
	}

	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	resp, _, err := f.fetch(ctx, rawURL)
	if err != nil {
		return nil, "", err
	}
	defer closeBody(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, "", &UpstreamError{Status: resp.StatusCode}
	}
	ct := mediaType(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(ct, "image/") {
		return nil, "", ErrNotImage
	}

	data, err = io.ReadAll(io.LimitReader(resp.Body, maxImageBody+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxImageBody {
		return nil, "", ErrTooLarge
	}

	f.imageCache.set(rawURL, &cachedImage{data: data, contentType: ct})
	return data, ct, nil
}

// fetch GETs u following at most maxRedirects redirects. Scheme, port and the
// resolved addresses are validated for the initial URL and again at every hop.
// It returns the final response and the URL it was fetched from; the caller
// owns the response body.
func (f *Fetcher) fetch(ctx context.Context, rawURL string) (*http.Response, *url.URL, error) {
	u, err := validateRequestURL(rawURL)
	if err != nil {
		return nil, nil, err
	}

	for hop := 0; ; hop++ {
		resp, err := f.doRequest(ctx, u)
		if err != nil {
			return nil, nil, err
		}
		if !isRedirect(resp.StatusCode) {
			return resp, u, nil
		}
		location := resp.Header.Get("Location")
		// Drain and close before the next hop so no connection leaks.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()

		if location == "" {
			return nil, nil, &UpstreamError{Status: resp.StatusCode}
		}
		next, err := u.Parse(location)
		if err != nil {
			return nil, nil, ErrInvalidURL
		}
		if err := checkURL(next); err != nil {
			return nil, nil, err
		}
		if hop >= maxRedirects {
			return nil, nil, ErrTooManyRedirects
		}
		u = next
	}
}

// doRequest issues one GET to u. The hostname is resolved up front, every
// returned address is validated and the transport then dials one of those
// validated addresses directly, so the name cannot be rebound between the
// check and the connect. For TLS the ServerName keeps the hostname so
// certificate validation is unaffected.
func (f *Fetcher) doRequest(ctx context.Context, u *url.URL) (*http.Response, error) {
	host := u.Hostname()
	addrs, err := f.lookupPublic(ctx, host)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		DisableKeepAlives:      true,
		ResponseHeaderTimeout:  headerTimeout,
		MaxResponseHeaderBytes: 64 << 10,
		TLSClientConfig: &tls.Config{
			ServerName: host,
			MinVersion: tls.VersionTLS12,
		},
		DialContext: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			port := u.Port()
			if _, p, splitErr := net.SplitHostPort(addr); splitErr == nil {
				port = p
			}
			if port == "" {
				if u.Scheme == "https" {
					port = "443"
				} else {
					port = "80"
				}
			}
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return nil, ErrInvalidURL
			}
			// Every entry in addrs passed the public-address check, so
			// any of them is a safe dial target.
			target := netip.AddrPortFrom(addrs[0], uint16(n))
			return f.dial(dialCtx, network, target.String())
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		// Redirects are followed manually so every hop is revalidated.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrInvalidURL
	}
	// A plain identity: nothing from the inbound client request is forwarded.
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,image/*;q=0.8,*/*;q=0.5")
	return client.Do(req)
}

// lookupPublic resolves host and requires every returned address to be a
// public global unicast address.
func (f *Fetcher) lookupPublic(ctx context.Context, host string) ([]netip.Addr, error) {
	addrs, err := f.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, ErrNoAddress
	}
	for _, addr := range addrs {
		if !publicAddr(addr) {
			return nil, ErrBlockedAddress
		}
	}
	return addrs, nil
}

// defaultResolve parses IP literals with netip, which rejects the non
// standard decimal, hex and octal forms, and falls back to DNS for names.
func defaultResolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		if addr, ok := netip.AddrFromSlice(ip.IP); ok {
			out = append(out, addr.WithZone(ip.Zone))
		}
	}
	return out, nil
}

// blockedPrefixes are address ranges that pass the generic public checks but
// are still not valid targets for outbound preview fetches.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),      // "this network", not routable
	netip.MustParsePrefix("100.64.0.0/10"),  // carrier grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),   // IETF protocol assignments
	netip.MustParsePrefix("240.0.0.0/4"),    // reserved, includes 255.255.255.255
	netip.MustParsePrefix("::/96"),          // deprecated IPv4 compatible form
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64 well known prefix
	netip.MustParsePrefix("64:ff9b:1::/48"), // NAT64 local use
	netip.MustParsePrefix("100::/64"),       // discard only
	netip.MustParsePrefix("2001::/23"),      // teredo, orchid and other special use
	netip.MustParsePrefix("2001:db8::/32"),  // documentation range
	netip.MustParsePrefix("192.0.2.0/24"),   // documentation
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
}

// publicAddr reports whether addr is safe to connect to: a public global
// unicast address. IPv4 mapped IPv6 addresses are normalised first so mapped
// forms of private addresses are rejected too.
func publicAddr(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// validateRequestURL parses raw and applies the target policy.
func validateRequestURL(raw string) (*url.URL, error) {
	if raw == "" || len(raw) > maxURLLength {
		return nil, ErrInvalidURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, ErrInvalidURL
	}
	if err := checkURL(u); err != nil {
		return nil, err
	}
	return u, nil
}

// checkURL enforces http/https, a present hostname, no userinfo and ports 80
// or 443 only.
func checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrInvalidURL
	}
	if u.User != nil {
		return ErrInvalidURL
	}
	if u.Hostname() == "" {
		return ErrInvalidURL
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || (n != 80 && n != 443) {
			return ErrInvalidURL
		}
	}
	return nil
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

// mediaType returns the lowercased media type without parameters.
func mediaType(contentType string) string {
	ct, _, found := strings.Cut(contentType, ";")
	if !found {
		ct = contentType
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

func mediaTypeIs(contentType, want string) bool {
	return mediaType(contentType) == want
}

func closeBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

type cachedImage struct {
	data        []byte
	contentType string
}

// cache is a small bounded map with per entry expiry.
type cache struct {
	mu    sync.Mutex
	ttl   time.Duration
	max   int
	items map[string]cacheEntry
}

type cacheEntry struct {
	value   any
	expires time.Time
}

func newCache(ttl time.Duration, max int) *cache {
	return &cache{ttl: ttl, max: max, items: make(map[string]cacheEntry)}
}

func (c *cache) get(key string) (any, bool) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[key]
	if !ok || now.After(entry.expires) {
		return nil, false
	}
	return entry.value, true
}

func (c *cache) set(key string, value any) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= c.max {
		for k, entry := range c.items {
			if now.After(entry.expires) {
				delete(c.items, k)
			}
		}
	}
	if len(c.items) >= c.max {
		// Still full: evict an arbitrary entry to stay bounded.
		for k := range c.items {
			delete(c.items, k)
			break
		}
	}
	c.items[key] = cacheEntry{value: value, expires: now.Add(c.ttl)}
}
