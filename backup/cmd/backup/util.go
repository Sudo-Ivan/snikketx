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
	if s.IntervalHours < 1 {
		s.IntervalHours = 1
	}
	if s.IntervalHours > 168 {
		s.IntervalHours = 168
	}
	if s.Scope != "data-only" {
		s.Scope = "full"
	}
	if s.KeepCount < 1 {
		s.KeepCount = 1
	}
	if s.KeepCount > 365 {
		s.KeepCount = 365
	}
	if s.KeepDays < 1 {
		s.KeepDays = 1
	}
	if s.KeepDays > 3650 {
		s.KeepDays = 3650
	}
	if s.ResticKeepLast < 1 {
		s.ResticKeepLast = 1
	}
	if s.ResticKeepDaily < 0 {
		s.ResticKeepDaily = 0
	}
	return s
}
