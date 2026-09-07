package health

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	pushHostAndroid = "push.snikket.net"
	pushHostIOS     = "push-ios.snikket.net"
	stunMagicCookie = 0x2112A442
)

var (
	xmlAttrEscaper = strings.NewReplacer(
		`&`, "&amp;",
		`'`, "&apos;",
		`"`, "&quot;",
		`<`, "&lt;",
		`>`, "&gt;",
	)
	probeDialer = &net.Dialer{Timeout: probeTimeout}
	probeTLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	stunBufPool = sync.Pool{
		New: func() any {
			b := make([]byte, 148)
			return &b
		},
	}
	startTLSNS     = []byte("urn:ietf:params:xml:ns:xmpp-tls")
	startTLSTag    = []byte("<starttls")
	streamErrorTag = []byte("<stream:error")
	errorTag       = []byte("<error")
	proceedTag     = []byte("<proceed")
	failureTag     = []byte("<failure")
)

// ProbeS2S dials TCP 5269 on the chat domain. Federation and mobile push both
// need this path. When the public dial fails but the Prosody service listens,
// the detail points at host firewall or DNS publish rather than Prosody itself.
func ProbeS2S(ctx context.Context, domain, prosodyEndpoint string) Component {
	start := time.Now()
	component := Component{Name: "s2s port 5269"}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		component.Detail = "domain not configured"
		component.Hint = "Set SNIKKET_DOMAIN"
		component.Latency = elapsed(start)
		return component
	}
	if isLocalDevHost(domain) {
		component.OK = true
		component.Detail = "skipped for local development host"
		component.Latency = elapsed(start)
		return component
	}

	publicErr := dialTCP(ctx, domain, "5269")
	if publicErr == nil {
		component.OK = true
		component.Detail = domain + ":5269 accepts TCP"
		component.Latency = elapsed(start)
		return component
	}

	internalHost := serviceHost(prosodyEndpoint)
	if internalHost != "" && !strings.EqualFold(internalHost, domain) {
		if dialTCP(ctx, internalHost, "5269") == nil {
			component.Detail = fmt.Sprintf("%s:5269 unreachable (%v), Prosody listens on %s:5269", domain, publicErr, internalHost)
			component.Hint = "Open TCP 5269 on the host firewall and confirm DNS for " + domain + " points at this host"
			component.Latency = elapsed(start)
			return component
		}
	}

	component.Detail = fmt.Sprintf("%s:5269 unreachable (%v)", domain, publicErr)
	component.Hint = "Open TCP 5269 on the host firewall (federation and push)"
	component.Latency = elapsed(start)
	return component
}

// ProbePush checks outbound TCP 5269 to the Snikket push hosts Prosody uses.
func ProbePush(ctx context.Context, domain string) Component {
	start := time.Now()
	component := Component{Name: "push path (s2s)"}
	if isLocalDevHost(strings.TrimSpace(domain)) {
		component.OK = true
		component.Detail = "skipped for local development host"
		component.Latency = elapsed(start)
		return component
	}

	hosts := []string{pushHostAndroid, pushHostIOS}
	var failed []string
	for _, host := range hosts {
		if err := dialTCP(ctx, host, "5269"); err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", host, err))
		}
	}
	component.Latency = elapsed(start)
	if len(failed) == 0 {
		component.OK = true
		component.Detail = "push.snikket.net and push-ios.snikket.net reachable on 5269"
		return component
	}
	component.Detail = "unreachable: " + strings.Join(failed, ", ")
	component.Hint = "Allow outbound TCP 5269 to push.snikket.net and push-ios.snikket.net (and fix DNS if they do not resolve)"
	return component
}

// ProbeTURN dials STUN/TURN TCP listeners and sends a STUN Binding over UDP.
func ProbeTURN(ctx context.Context, domain, prosodyEndpoint string) Component {
	start := time.Now()
	component := Component{Name: "TURN / STUN"}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		component.Detail = "domain not configured"
		component.Hint = "Set SNIKKET_DOMAIN"
		component.Latency = elapsed(start)
		return component
	}
	if isLocalDevHost(domain) {
		component.OK = true
		component.Detail = "skipped for local development host"
		component.Latency = elapsed(start)
		return component
	}

	target := domain
	tcp3478 := dialTCP(ctx, target, "3478")
	tcp5349 := dialTCP(ctx, target, "5349")
	udpSTUN := stunBinding(ctx, target, "3478")

	if tcp3478 != nil || tcp5349 != nil || udpSTUN != nil {
		internalHost := serviceHost(prosodyEndpoint)
		if internalHost != "" && !strings.EqualFold(internalHost, domain) {
			if dialTCP(ctx, internalHost, "3478") == nil {
				target = internalHost
				tcp3478 = dialTCP(ctx, target, "3478")
				tcp5349 = dialTCP(ctx, target, "5349")
				udpSTUN = stunBinding(ctx, target, "3478")
				if tcp3478 == nil && tcp5349 == nil && udpSTUN == nil {
					component.Detail = "coturn listens inside the stack but " + domain + " TURN ports are unreachable"
					component.Hint = "Open UDP/TCP 3478 and TCP 5349 on the host firewall, confirm DNS for " + domain
					component.Latency = elapsed(start)
					return component
				}
			}
		}
	}

	component.Latency = elapsed(start)
	var problems []string
	if tcp3478 != nil {
		problems = append(problems, "TCP 3478: "+tcp3478.Error())
	}
	if udpSTUN != nil {
		problems = append(problems, "UDP STUN 3478: "+udpSTUN.Error())
	}
	if tcp5349 != nil {
		problems = append(problems, "TCP 5349: "+tcp5349.Error())
	}
	if len(problems) == 0 {
		component.OK = true
		component.Detail = target + " answers STUN on UDP 3478 and TCP 3478/5349"
		return component
	}
	component.Detail = strings.Join(problems, ", ")
	component.Hint = "Open UDP/TCP 3478 and TCP 5349 (TURN TLS). Check coturn is running"
	return component
}

// ProbeXMPPTLS upgrades a c2s connection with STARTTLS and reads the Prosody
// leaf certificate. This is distinct from RavenGuard HTTPS on port 443.
func ProbeXMPPTLS(ctx context.Context, domain, prosodyEndpoint string) Component {
	start := time.Now()
	component := Component{Name: "XMPP TLS (Prosody)"}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		component.Detail = "domain not configured"
		component.Hint = "Set SNIKKET_DOMAIN"
		component.Latency = elapsed(start)
		return component
	}
	if isLocalDevHost(domain) {
		component.OK = true
		component.Detail = "skipped for local development host"
		component.Latency = elapsed(start)
		return component
	}

	cert, err := xmppStartTLSCert(ctx, domain, domain)
	if err != nil {
		internalHost := serviceHost(prosodyEndpoint)
		if internalHost != "" && !strings.EqualFold(internalHost, domain) {
			cert, err = xmppStartTLSCert(ctx, internalHost, domain)
			if err == nil {
				component.Detail = fmt.Sprintf("Prosody cert via %s:5222 (domain:5222 not reachable from portal)", internalHost)
				fillCertDetail(&component, cert)
				component.Hint = "Open TCP 5222 on the host firewall if clients connect from the internet"
				component.Latency = elapsed(start)
				return component
			}
		}
		component.Detail = err.Error()
		component.Hint = "Open TCP 5222 and confirm Prosody has a valid Let's Encrypt cert under certs/"
		component.Latency = elapsed(start)
		return component
	}

	fillCertDetail(&component, cert)
	component.Latency = elapsed(start)
	return component
}

func fillCertDetail(component *Component, cert peerCert) {
	component.OK = cert.DaysLeft > 0
	component.Detail = fmt.Sprintf("%s, expires %s (%d days)", cert.CommonName, cert.Expires, cert.DaysLeft)
	if cert.DaysLeft <= 14 && cert.DaysLeft > 0 {
		component.Detail += ", renew soon"
		component.Hint = "Renew the XMPP certificate (cert-manager / Prosody certs), not only the HTTPS edge cert"
	}
	if cert.DaysLeft <= 0 {
		component.OK = false
		component.Detail += ", expired"
		component.Hint = "Renew the XMPP certificate under Prosody certs (distinct from RavenGuard :443)"
	}
}

type peerCert struct {
	CommonName string
	Expires    string
	DaysLeft   int
}

func xmppStartTLSCert(ctx context.Context, dialHost, serverName string) (peerCert, error) {
	var zero peerCert
	conn, err := probeDialer.DialContext(ctx, "tcp", net.JoinHostPort(dialHost, "5222"))
	if err != nil {
		return zero, fmt.Errorf("dial %s:5222: %w", dialHost, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(probeTimeout))

	var open strings.Builder
	open.Grow(128 + len(serverName))
	open.WriteString("<?xml version='1.0'?><stream:stream to='")
	open.WriteString(xmlEscapeAttr(serverName))
	open.WriteString("' xmlns='jabber:client' xmlns:stream='http://etherx.jabber.org/streams' version='1.0'>")
	if _, err := io.WriteString(conn, open.String()); err != nil {
		return zero, fmt.Errorf("stream open: %w", err)
	}

	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 2048)
	sawTLS := false
	for deadline := time.Now().Add(probeTimeout); time.Now().Before(deadline); {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if containsASCIIFold(buf, startTLSNS) || containsASCIIFold(buf, startTLSTag) {
				sawTLS = true
				break
			}
			if containsASCIIFold(buf, streamErrorTag) || containsASCIIFold(buf, errorTag) {
				return zero, fmt.Errorf("stream error from %s", dialHost)
			}
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if sawTLS || len(buf) > 0 {
					break
				}
				continue
			}
			return zero, fmt.Errorf("read features: %w", err)
		}
	}
	if !sawTLS {
		return zero, fmt.Errorf("no STARTTLS offer on %s:5222", dialHost)
	}

	if _, err := io.WriteString(conn, "<starttls xmlns='urn:ietf:params:xml:ns:xmpp-tls'/>"); err != nil {
		return zero, fmt.Errorf("starttls request: %w", err)
	}

	buf = buf[:0]
	proceed := false
	for deadline := time.Now().Add(probeTimeout); time.Now().Before(deadline); {
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if containsASCIIFold(buf, proceedTag) {
				proceed = true
				break
			}
			if containsASCIIFold(buf, failureTag) {
				return zero, fmt.Errorf("STARTTLS failure from %s", dialHost)
			}
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return zero, fmt.Errorf("read proceed: %w", err)
		}
	}
	if !proceed {
		return zero, fmt.Errorf("no STARTTLS proceed from %s", dialHost)
	}

	_ = conn.SetDeadline(time.Now().Add(probeTimeout))
	tlsCfg := probeTLSConfig.Clone()
	tlsCfg.ServerName = serverName
	tlsConn := tls.Client(conn, tlsCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return zero, fmt.Errorf("TLS handshake: %w", err)
	}
	defer tlsConn.Close()

	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return zero, fmt.Errorf("no peer certificate")
	}
	leaf := state.PeerCertificates[0]
	days := int(time.Until(leaf.NotAfter).Hours() / 24)
	return peerCert{
		CommonName: leaf.Subject.CommonName,
		Expires:    leaf.NotAfter.UTC().Format("2006-01-02"),
		DaysLeft:   days,
	}, nil
}

// containsASCIIFold reports whether haystack contains needle, ignoring ASCII case.
func containsASCIIFold(haystack, needle []byte) bool {
	n := len(needle)
	if n == 0 {
		return true
	}
outer:
	for i := 0; i+n <= len(haystack); i++ {
		for j := 0; j < n; j++ {
			c := haystack[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != needle[j] {
				continue outer
			}
		}
		return true
	}
	return false
}

func dialTCP(ctx context.Context, host, port string) error {
	conn, err := probeDialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

func stunBinding(ctx context.Context, host, port string) error {
	conn, err := probeDialer.DialContext(ctx, "udp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	defer conn.Close()

	bufp := stunBufPool.Get().(*[]byte)
	buf := *bufp
	req := buf[:20]
	clear(req)
	req[0], req[1] = 0x00, 0x01
	binary.BigEndian.PutUint32(req[4:8], stunMagicCookie)
	if _, err := rand.Read(req[8:20]); err != nil {
		stunBufPool.Put(bufp)
		return err
	}

	_ = conn.SetDeadline(time.Now().Add(probeTimeout))
	if _, err := conn.Write(req); err != nil {
		stunBufPool.Put(bufp)
		return err
	}
	resp := buf[20:148]
	n, err := conn.Read(resp)
	if err != nil {
		stunBufPool.Put(bufp)
		return err
	}
	if n < 20 {
		stunBufPool.Put(bufp)
		return fmt.Errorf("short STUN response")
	}
	if resp[0] != 0x01 || resp[1] != 0x01 {
		stunBufPool.Put(bufp)
		return fmt.Errorf("unexpected STUN message type %02x%02x", resp[0], resp[1])
	}
	if binary.BigEndian.Uint32(resp[4:8]) != stunMagicCookie {
		stunBufPool.Put(bufp)
		return fmt.Errorf("bad STUN magic cookie")
	}
	stunBufPool.Put(bufp)
	return nil
}

func serviceHost(prosodyEndpoint string) string {
	u, err := url.Parse(strings.TrimSpace(prosodyEndpoint))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return u.Hostname()
}

func elapsed(start time.Time) string {
	return time.Since(start).Round(time.Millisecond).String()
}

func xmlEscapeAttr(s string) string {
	return xmlAttrEscaper.Replace(s)
}

// FormatComponentDetail joins Detail and Hint for display surfaces.
func FormatComponentDetail(c Component) string {
	detail := strings.TrimSpace(c.Detail)
	hint := strings.TrimSpace(c.Hint)
	switch {
	case detail == "" && hint == "":
		return ""
	case hint == "":
		return detail
	case detail == "":
		return hint
	case c.OK:
		return detail
	default:
		return detail + ". " + hint
	}
}
