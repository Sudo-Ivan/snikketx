package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

var jsonBufPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 1024))
	},
}

func writeJSON(w http.ResponseWriter, v any) {
	buf := jsonBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		jsonBufPool.Put(buf)
		http.Error(w, "encode failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
	if buf.Cap() <= 1<<20 {
		jsonBufPool.Put(buf)
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func readTrimmed(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	// #nosec G304 -- operator-configured password file path
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func normalizeSettings(s settings) settings {
	if s.IntervalHours < minIntervalHours {
		s.IntervalHours = minIntervalHours
	}
	if s.IntervalHours > maxIntervalHours {
		s.IntervalHours = maxIntervalHours
	}
	if s.Scope != scopeDataOnly {
		s.Scope = scopeFull
	}
	if s.KeepCount < minKeepCount {
		s.KeepCount = minKeepCount
	}
	if s.KeepCount > maxKeepCount {
		s.KeepCount = maxKeepCount
	}
	if s.KeepDays < minKeepDays {
		s.KeepDays = minKeepDays
	}
	if s.KeepDays > maxKeepDays {
		s.KeepDays = maxKeepDays
	}
	if s.ResticKeepLast < minResticKeepLast {
		s.ResticKeepLast = minResticKeepLast
	}
	if s.ResticKeepDaily < minResticKeepDaily {
		s.ResticKeepDaily = minResticKeepDaily
	}
	return s
}
