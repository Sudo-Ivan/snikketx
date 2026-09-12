package handlers

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// xmppProxyPath reports whether a request path is one of the anonymous XMPP
// protocol endpoints that may be relayed to Prosody. The allowlist is checked
// again inside the proxy so nothing else on the internal HTTP port (upload,
// administration, metrics) can be reached through the edge.
func xmppProxyPath(path string) bool {
	switch path {
	case "/xmpp-websocket", "/http-bind",
		"/.well-known/host-meta", "/.well-known/host-meta.json":
		return true
	}
	return strings.HasPrefix(path, "/xmpp-websocket/") ||
		strings.HasPrefix(path, "/http-bind/")
}

// mountXMPPProxy mounts the anonymous XMPP protocol endpoints that external
// clients such as ConverseJS reach through the TLS edge: the WebSocket
// endpoint, the BOSH endpoint and the host-meta discovery documents served by
// mod_http_altconnect. These routes carry their own authentication inside the
// XMPP protocol, so no portal session is required and CSRF validation does not
// apply.
func (a *App) mountXMPPProxy(mux *http.ServeMux) {
	target, err := url.Parse(a.Cfg.ProsodyEndpoint)
	if err != nil || target.Scheme == "" || target.Host == "" {
		slog.Error("xmpp proxy disabled, invalid prosody endpoint",
			slog.String("endpoint", a.Cfg.ProsodyEndpoint))
		return
	}

	// ResponseHeaderTimeout stays disabled: BOSH holds a request open for the
	// negotiated wait window before answering, and a WebSocket upgrade only
	// reads headers during the handshake.
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          256,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 5 * time.Second,
	}

	proxy := &httputil.ReverseProxy{
		Transport: transport,
		// Flush after every write so BOSH responses and upgrade traffic are
		// relayed without latency.
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
			// The portal sits behind the TLS edge and never terminates TLS
			// itself, so keep the scheme the edge reported. Prosody uses it
			// for the secure session flags and for the wss:// URI published
			// in host-meta.
			if proto := pr.In.Header.Get("X-Forwarded-Proto"); proto != "" {
				pr.Out.Header.Set("X-Forwarded-Proto", proto)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			id := a.recordError(r, err)
			slog.Warn("xmpp proxy upstream failure",
				slog.String("request_id", requestID(r)),
				slog.String("error_id", id),
				slog.String("path", r.URL.Path),
				slog.String("error", err.Error()),
			)
			http.Error(w, "The chat server could not be reached.", http.StatusBadGateway)
		},
	}

	protocol := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !xmppProxyPath(r.URL.Path) {
			a.notFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	})

	// Registered without a method so WebSocket upgrades, BOSH POSTs and CORS
	// preflights all reach Prosody unchanged.
	mux.Handle("/xmpp-websocket", protocol)
	mux.Handle("/xmpp-websocket/", protocol)
	mux.Handle("/http-bind", protocol)
	mux.Handle("/http-bind/", protocol)
	mux.Handle("/.well-known/host-meta", protocol)
	mux.Handle("/.well-known/host-meta.json", protocol)
}
