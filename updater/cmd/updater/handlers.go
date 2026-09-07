package main

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), s.tokenBytes) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.snapshot())
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, s.settings)
}

func (s *server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var next settings
	if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if next.IntervalHours < 1 {
		next.IntervalHours = 1
	}
	if next.IntervalHours > 168 {
		next.IntervalHours = 168
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	if err := s.saveSettings(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !next.PinDigests {
		_ = s.writePinOverride(map[string]string{})
	} else {
		s.mu.Lock()
		pins := copyMap(s.pins)
		s.mu.Unlock()
		_ = s.writePinOverride(pins)
	}
	writeJSON(w, next)
}

func (s *server) handleGetPins(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, pinsFile{Services: copyMap(s.pins)})
}

func (s *server) handlePutPins(w http.ResponseWriter, r *http.Request) {
	var body pinsFile
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Services == nil {
		body.Services = map[string]string{}
	}
	s.mu.Lock()
	s.pins = body.Services
	pinMode := s.settings.PinDigests
	s.mu.Unlock()
	if err := s.savePins(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pinMode {
		_ = s.writePinOverride(body.Services)
	}
	writeJSON(w, body)
}

func (s *server) handleClearPins(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.pins = map[string]string{}
	s.mu.Unlock()
	_ = s.savePins()
	_ = s.writePinOverride(map[string]string{})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleCheck(w http.ResponseWriter, r *http.Request) {
	if err := s.startJob("check", s.runCheckJob); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, s.snapshot())
}

func (s *server) handleApply(w http.ResponseWriter, r *http.Request) {
	if err := s.startJob("apply", s.runApplyJob); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, s.snapshot())
}

func (s *server) handleLogs(w http.ResponseWriter, r *http.Request) {
	serviceID := strings.TrimSpace(r.URL.Query().Get("service"))
	tail := clampTail(r.URL.Query().Get("tail"))
	if serviceID == "" {
		writeJSON(w, logsResponse{Tail: tail, Services: allowedLogServices})
		return
	}
	svc, ok := resolveLogService(serviceID)
	if !ok {
		http.Error(w, "unknown or disallowed service", http.StatusBadRequest)
		return
	}
	raw, err := s.fetchComposeLogs(r.Context(), svc.ID, tail)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	rawLines, truncated := splitLogLines(raw, 512<<10)
	entries, lines := buildLogEntries(rawLines)
	writeJSON(w, logsResponse{
		Service:   svc.ID,
		Label:     svc.Label,
		Tail:      tail,
		Lines:     lines,
		Entries:   entries,
		Truncated: truncated,
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
		Services:  allowedLogServices,
	})
}
