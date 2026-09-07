package health

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/hostmetrics"
	"github.com/sudo-ivan/snikketx/web-portal/internal/webui"
)

const probeTimeout = 4 * time.Second

// ProbeDomain resolves the configured domain name.
func ProbeDomain(ctx context.Context, domain string) Component {
	start := time.Now()
	component := Component{Name: "domain DNS"}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		component.Detail = "domain not configured"
		component.Latency = time.Since(start).Round(time.Millisecond).String()
		return component
	}

	resolver := net.DefaultResolver
	addrs, err := resolver.LookupHost(ctx, domain)
	component.Latency = time.Since(start).Round(time.Millisecond).String()
	if err != nil {
		component.Detail = err.Error()
		return component
	}
	component.OK = true
	if len(addrs) > 3 {
		addrs = addrs[:3]
	}
	component.Detail = domain + " resolves to " + strings.Join(addrs, ", ")
	return component
}

// ProbeTLS dials the domain on port 443 and reports certificate validity.
func ProbeTLS(ctx context.Context, domain string) Component {
	start := time.Now()
	component := Component{Name: "TLS certificate"}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		component.Detail = "domain not configured"
		component.Latency = time.Since(start).Round(time.Millisecond).String()
		return component
	}
	if isLocalDevHost(domain) {
		component.OK = true
		component.Detail = "skipped for local development host"
		component.Latency = time.Since(start).Round(time.Millisecond).String()
		return component
	}

	dialer := &net.Dialer{Timeout: probeTimeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(domain, "443"), &tls.Config{
		ServerName: domain,
		MinVersion: tls.VersionTLS12,
	})
	component.Latency = time.Since(start).Round(time.Millisecond).String()
	if err != nil {
		component.Detail = err.Error()
		return component
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		component.Detail = "no peer certificate"
		return component
	}
	cert := state.PeerCertificates[0]
	days := int(time.Until(cert.NotAfter).Hours() / 24)
	component.OK = days > 0
	component.Detail = fmt.Sprintf("%s, expires %s (%d days)", cert.Subject.CommonName, cert.NotAfter.UTC().Format("2006-01-02"), days)
	if days <= 14 && days > 0 {
		component.Detail += ", renew soon"
	}
	if days <= 0 {
		component.OK = false
		component.Detail += ", expired"
	}
	return component
}

// ProbeHTTPS fetches https://domain/ and reports reachability.
func ProbeHTTPS(ctx context.Context, domain string) Component {
	start := time.Now()
	component := Component{Name: "HTTPS"}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		component.Detail = "domain not configured"
		component.Latency = time.Since(start).Round(time.Millisecond).String()
		return component
	}
	if isLocalDevHost(domain) {
		component.OK = true
		component.Detail = "skipped for local development host"
		component.Latency = time.Since(start).Round(time.Millisecond).String()
		return component
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/", nil)
	if err != nil {
		component.Detail = err.Error()
		component.Latency = time.Since(start).Round(time.Millisecond).String()
		return component
	}
	client := &http.Client{
		Timeout: probeTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	component.Latency = time.Since(start).Round(time.Millisecond).String()
	if err != nil {
		component.Detail = err.Error()
		return component
	}
	defer resp.Body.Close()
	component.OK = resp.StatusCode > 0 && resp.StatusCode < 600
	component.Detail = fmt.Sprintf("answered HTTP %d", resp.StatusCode)
	return component
}

// ProbeMemory reports host memory fill from MemAvailable.
func ProbeMemory(stats hostmetrics.Stats) Component {
	component := Component{Name: "memory"}
	if stats.MemTotal == nil || stats.MemAvailable == nil || stats.MemUsedRatio == nil {
		component.Detail = "memory counters unavailable"
		return component
	}
	component.OK = *stats.MemUsedRatio < 0.92
	component.Detail = fmt.Sprintf("%s free of %s (%s used)",
		webui.FormatBytes(*stats.MemAvailable),
		webui.FormatBytes(*stats.MemTotal),
		webui.FormatPercent(*stats.MemUsedRatio),
	)
	if *stats.MemUsedRatio >= 0.92 {
		component.Detail += ", critically low free memory"
	}
	return component
}

// ProbeCPU reports load average relative to CPU count.
func ProbeCPU(stats hostmetrics.Stats) Component {
	component := Component{Name: "CPU load"}
	if stats.Load5 == nil || stats.NumCPU < 1 {
		component.Detail = "load average unavailable"
		return component
	}
	perCPU := *stats.Load5 / float64(stats.NumCPU)
	component.OK = perCPU < 2.0
	detail := fmt.Sprintf("load 1/5/15 = %s / %s / %s across %d CPU",
		formatLoad(stats.Load1), formatLoad(stats.Load5), formatLoad(stats.Load15), stats.NumCPU)
	if stats.PortalCPU != nil {
		detail += ", portal " + webui.FormatPercent(*stats.PortalCPU)
	}
	component.Detail = detail
	if perCPU >= 2.0 {
		component.Detail += ", sustained overload"
	}
	return component
}

// ProbePressure reports Linux PSI pressure averages when available.
func ProbePressure(stats hostmetrics.Stats) Component {
	component := Component{Name: "resource pressure"}
	if stats.CPUPressure10 == nil && stats.MemPressure10 == nil && stats.IOPressure10 == nil {
		component.OK = true
		component.Detail = "PSI unavailable on this host"
		return component
	}
	parts := make([]string, 0, 3)
	ok := true
	if stats.CPUPressure10 != nil {
		parts = append(parts, fmt.Sprintf("cpu %.2f%%", *stats.CPUPressure10))
		if *stats.CPUPressure10 >= 20 {
			ok = false
		}
	}
	if stats.MemPressure10 != nil {
		parts = append(parts, fmt.Sprintf("memory %.2f%%", *stats.MemPressure10))
		if *stats.MemPressure10 >= 10 {
			ok = false
		}
	}
	if stats.IOPressure10 != nil {
		parts = append(parts, fmt.Sprintf("io %.2f%%", *stats.IOPressure10))
		if *stats.IOPressure10 >= 20 {
			ok = false
		}
	}
	component.OK = ok
	component.Detail = "10s some averages: " + strings.Join(parts, ", ")
	if !ok {
		component.Detail += ", elevated stall time"
	}
	return component
}

func formatLoad(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", *v)
}

func isLocalDevHost(domain string) bool {
	d := strings.ToLower(domain)
	return d == "localhost" || strings.HasSuffix(d, ".localhost") || net.ParseIP(d) != nil
}
