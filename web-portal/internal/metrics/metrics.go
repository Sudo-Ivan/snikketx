package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/hostmetrics"
)

type Registry struct {
	started time.Time
	reqs    sync.Map // route group -> *counters
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
	// stored lightly via sync.Map key
	r.reqs.Store("__prosody_cache__", m)
}

func (r *Registry) Handler(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if token != "" {
			got := req.Header.Get("Authorization")
			if got != "Bearer "+token && req.URL.Query().Get("token") != token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		stats := hostmetrics.Collect()
		var b strings.Builder
		fmt.Fprintf(&b, "# HELP portal_up Portal process up\n# TYPE portal_up gauge\nportal_up 1\n")
		fmt.Fprintf(&b, "# HELP portal_uptime_seconds Portal uptime\n# TYPE portal_uptime_seconds gauge\nportal_uptime_seconds %.3f\n", time.Since(r.started).Seconds())
		if stats.PortalRSS != nil {
			fmt.Fprintf(&b, "# HELP portal_memory_rss_bytes Resident memory\n# TYPE portal_memory_rss_bytes gauge\nportal_memory_rss_bytes %d\n", *stats.PortalRSS)
		}
		if stats.PortalCPU != nil {
			fmt.Fprintf(&b, "# HELP portal_cpu_ratio Process CPU ratio\n# TYPE portal_cpu_ratio gauge\nportal_cpu_ratio %.6f\n", *stats.PortalCPU)
		}
		fmt.Fprintf(&b, "# HELP portal_goroutines Goroutine count\n# TYPE portal_goroutines gauge\nportal_goroutines %d\n", stats.Goroutines)
		if stats.Load5 != nil {
			fmt.Fprintf(&b, "# HELP node_load5 Load average 5m\n# TYPE node_load5 gauge\nnode_load5 %.3f\n", *stats.Load5)
		}
		fmt.Fprintf(&b, "# HELP portal_http_requests_total HTTP requests\n# TYPE portal_http_requests_total counter\n")
		r.reqs.Range(func(key, value any) bool {
			k, ok := key.(string)
			if !ok || strings.HasPrefix(k, "__") {
				return true
			}
			c := value.(*counters)
			fmt.Fprintf(&b, "portal_http_requests_total{group=%q} %d\n", k, c.count.Load())
			return true
		})
		fmt.Fprintf(&b, "# HELP portal_http_request_duration_seconds_sum Request duration sum\n# TYPE portal_http_request_duration_seconds_sum counter\n")
		r.reqs.Range(func(key, value any) bool {
			k, ok := key.(string)
			if !ok || strings.HasPrefix(k, "__") {
				return true
			}
			c := value.(*counters)
			fmt.Fprintf(&b, "portal_http_request_duration_seconds_sum{group=%q} %.6f\n", k, float64(c.sumNS.Load())/1e9)
			return true
		})
		if v, ok := r.reqs.Load("__prosody_cache__"); ok {
			if m, ok := v.(map[string]any); ok {
				writeProsody(&b, m)
			}
		}
		_, _ = w.Write([]byte(b.String()))
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
