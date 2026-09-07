package health

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"
)

// CertHost is one hostname checked for TLS expiry and DNS records.
type CertHost struct {
	Host      string
	Role      string
	OK        bool
	Detail    string
	Expires   string
	DaysLeft  int
	Addresses []string
	CAA       []string
	SRV       []string
	Latency   string
}

// ProbeCertHost resolves DNS, optional SRV and CAA, and reads the TLS leaf.
func ProbeCertHost(ctx context.Context, host, role string) CertHost {
	start := time.Now()
	out := CertHost{Host: host, Role: role}
	host = strings.TrimSpace(host)
	if host == "" {
		out.Detail = "host not configured"
		out.Latency = time.Since(start).Round(time.Millisecond).String()
		return out
	}
	if isLocalDevHost(host) {
		out.OK = true
		out.Detail = "skipped for local development host"
		out.Latency = time.Since(start).Round(time.Millisecond).String()
		return out
	}

	resolver := net.DefaultResolver
	addrs, err := resolver.LookupHost(ctx, host)
	if err != nil {
		out.Detail = "DNS: " + err.Error()
		out.Latency = time.Since(start).Round(time.Millisecond).String()
		return out
	}
	if len(addrs) > 4 {
		addrs = addrs[:4]
	}
	out.Addresses = addrs

	for _, service := range []struct{ name, proto string }{
		{"xmpp-client", "tcp"},
		{"xmpp-server", "tcp"},
	} {
		_, srvs, err := resolver.LookupSRV(ctx, service.name, service.proto, host)
		if err != nil {
			continue
		}
		for _, srv := range srvs {
			out.SRV = append(out.SRV, fmt.Sprintf("%s.%s %s:%d p=%d w=%d",
				service.name, service.proto, strings.TrimSuffix(srv.Target, "."), srv.Port, srv.Priority, srv.Weight))
		}
	}
	if caa := probeCAA(ctx, host); len(caa) > 0 {
		out.CAA = caa
	}

	dialer := probeDialer
	tlsCfg := probeTLSConfig.Clone()
	tlsCfg.ServerName = host
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, "443"), tlsCfg)
	out.Latency = time.Since(start).Round(time.Millisecond).String()
	if err != nil {
		out.Detail = "TLS: " + err.Error()
		return out
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		out.Detail = "no peer certificate"
		return out
	}
	cert := state.PeerCertificates[0]
	days := int(time.Until(cert.NotAfter).Hours() / 24)
	out.DaysLeft = days
	out.Expires = cert.NotAfter.UTC().Format(time.RFC3339)
	out.OK = days > 0
	out.Detail = fmt.Sprintf("%s, expires %s (%d days)", cert.Subject.CommonName, cert.NotAfter.UTC().Format("2006-01-02"), days)
	if days <= 14 && days > 0 {
		out.Detail += ", renew soon"
	}
	if days <= 0 {
		out.OK = false
		out.Detail += ", expired"
	}
	return out
}

// ProbeXMPPCertHost wraps ProbeXMPPTLS for the certificates page.
func ProbeXMPPCertHost(ctx context.Context, domain, prosodyEndpoint string) CertHost {
	start := time.Now()
	c := ProbeXMPPTLS(ctx, domain, prosodyEndpoint)
	out := CertHost{
		Host:    strings.TrimSpace(domain) + ":5222",
		Role:    "XMPP Prosody STARTTLS",
		OK:      c.OK,
		Detail:  FormatComponentDetail(c),
		Latency: c.Latency,
	}
	if out.Latency == "" {
		out.Latency = time.Since(start).Round(time.Millisecond).String()
	}
	if i := strings.LastIndex(c.Detail, "("); i >= 0 {
		var days int
		if _, err := fmt.Sscanf(c.Detail[i:], "(%d days)", &days); err == nil {
			out.DaysLeft = days
		}
	}
	return out
}

func probeCAA(ctx context.Context, host string) []string {
	r := &net.Resolver{}
	txts, err := r.LookupTXT(ctx, host)
	if err != nil {
		return nil
	}
	var out []string
	for _, txt := range txts {
		if strings.Contains(strings.ToLower(txt), "issue") {
			out = append(out, txt)
		}
	}
	return out
}
