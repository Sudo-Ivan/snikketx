package handlers

import "net/http"

// mountMetrics registers the Prometheus exposition endpoint. It stays absent
// when the operator disabled metrics, so nothing is published by accident.
func (a *App) mountMetrics(mux *http.ServeMux) {
	if a.Metrics == nil || !a.Cfg.ShowMetrics {
		return
	}
	handler := a.Metrics.Handler(a.Cfg.MetricsToken)
	mux.Handle("GET /metrics", handler)
}
