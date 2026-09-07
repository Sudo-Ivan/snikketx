package metrics

import (
	"crypto/subtle"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/hostmetrics"
)

type Registry struct {
	started time.Time
	reqs    sync.Map
	cacheMu sync.RWMutex
	cache   map[string]any
}

type counters struct {
	count atomic.Int64
	sumNS atomic.Int64
}

func New() *Registry {
	return &Registry{started: time.Now()}
}

func (r *Registry) Observe(group string, d time.Duration) {
	if group == "" {
		group = "other"
	}
	v, _ := r.reqs.LoadOrStore(group, &counters{})
	c := v.(*counters)
	c.count.Add(1)
	c.sumNS.Add(d.Nanoseconds())
}

func (r *Registry) SetProsodyCache(m map[string]any) {
	cp := make(map[string]any, len(m))
	maps.Copy(cp, m)
	r.cacheMu.Lock()
	r.cache = cp
	r.cacheMu.Unlock()
}

func (r *Registry) Handler(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// Metrics stay closed unless the operator sets a scrape token.
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		got := req.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(got) < len(prefix) || !strings.HasPrefix(got, prefix) ||
			subtle.ConstantTimeCompare([]byte(got[len(prefix):]), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		stats := hostmetrics.Collect()
		bw := &strings.Builder{}
		bw.Grow(1024)
		fmt.Fprintf(bw, "# HELP portal_up Portal process up\n# TYPE portal_up gauge\nportal_up 1\n")
		fmt.Fprintf(bw, "# HELP portal_uptime_seconds Portal uptime\n# TYPE portal_uptime_seconds gauge\nportal_uptime_seconds %.3f\n", time.Since(r.started).Seconds())
		if stats.PortalRSS != nil {
			fmt.Fprintf(bw, "# HELP portal_memory_rss_bytes Resident memory\n# TYPE portal_memory_rss_bytes gauge\nportal_memory_rss_bytes %d\n", *stats.PortalRSS)
		}
		if stats.PortalCPU != nil {
			fmt.Fprintf(bw, "# HELP portal_cpu_ratio Process CPU ratio\n# TYPE portal_cpu_ratio gauge\nportal_cpu_ratio %.6f\n", *stats.PortalCPU)
		}
		fmt.Fprintf(bw, "# HELP portal_goroutines Goroutine count\n# TYPE portal_goroutines gauge\nportal_goroutines %d\n", stats.Goroutines)
		if stats.Load5 != nil {
			fmt.Fprintf(bw, "# HELP node_load5 Load average 5m\n# TYPE node_load5 gauge\nnode_load5 %.3f\n", *stats.Load5)
		}
		fmt.Fprintf(bw, "# HELP portal_http_requests_total HTTP requests\n# TYPE portal_http_requests_total counter\n")
		type snap struct {
			group string
			count int64
			sumNS int64
		}
		var snaps []snap
		r.reqs.Range(func(key, value any) bool {
			k, ok := key.(string)
			if !ok || strings.HasPrefix(k, "__") {
				return true
			}
			c := value.(*counters)
			snaps = append(snaps, snap{group: k, count: c.count.Load(), sumNS: c.sumNS.Load()})
			return true
		})
		for _, s := range snaps {
			fmt.Fprintf(bw, "portal_http_requests_total{group=%q} %d\n", s.group, s.count)
		}
		fmt.Fprintf(bw, "# HELP portal_http_request_duration_seconds_sum Request duration sum\n# TYPE portal_http_request_duration_seconds_sum counter\n")
		for _, s := range snaps {
			fmt.Fprintf(bw, "portal_http_request_duration_seconds_sum{group=%q} %.6f\n", s.group, float64(s.sumNS)/1e9)
		}
		r.cacheMu.RLock()
		cache := r.cache
		r.cacheMu.RUnlock()
		if cache != nil {
			writeProsody(bw, cache)
		}
		_, _ = io.WriteString(w, bw.String())
	}
}

func writeProsody(b *strings.Builder, m map[string]any) {
	if v, ok := asFloat(m["memory"]); ok {
		fmt.Fprintf(b, "# HELP prosody_memory_rss_bytes Prosody RSS\n# TYPE prosody_memory_rss_bytes gauge\nprosody_memory_rss_bytes %.0f\n", v)
	}
	if v, ok := asFloat(m["c2s"]); ok {
		fmt.Fprintf(b, "# HELP prosody_c2s_connections Connected devices\n# TYPE prosody_c2s_connections gauge\nprosody_c2s_connections %.0f\n", v)
	}
	if v, ok := asFloat(m["uploads"]); ok {
		fmt.Fprintf(b, "# HELP prosody_uploads_bytes Upload storage\n# TYPE prosody_uploads_bytes gauge\nprosody_uploads_bytes %.0f\n", v)
	}
	if users, ok := m["users"].(map[string]any); ok {
		for _, k := range []string{"active_1d", "active_7d", "active_30d"} {
			if v, ok := asFloat(users[k]); ok {
				fmt.Fprintf(b, "prosody_users_active{window=%q} %.0f\n", k, v)
			}
		}
	}
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
