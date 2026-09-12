package main

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"path/filepath"
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
	next = normalizeSettings(next)
	if err := s.saveSettings(next); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, next)
}

func (s *server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if err := s.startJob("backup", s.runBackupJob); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, s.snapshot())
}

func (s *server) handleArchives(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"archives": s.listArchives()})
}

type dryRunRequest struct {
	Archive string `json:"archive"`
}

func (s *server) handleDryRun(w http.ResponseWriter, r *http.Request) {
	var body dryRunRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := filepath.Base(strings.TrimSpace(body.Archive))
	if name == "" || name == "." || name == "/" {
		http.Error(w, "archive name required", http.StatusBadRequest)
		return
	}
	path := filepath.Join(s.archiveDir, name)
	writeJSON(w, s.dryRunRestore(path))
}

func (s *server) handleResticCheck(w http.ResponseWriter, r *http.Request) {
	out, err := s.resticCheck(r.Context())
	if err != nil {
		http.Error(w, err.Error()+": "+out, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "detail": strings.TrimSpace(out)})
}

func (s *server) snapshot() statusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := "idle"
	phase := s.phase
	if phase == "" {
		phase = "idle"
	}
	if s.busy {
		status = "busy"
	}
	archives := s.listArchivesLocked()
	resp := statusResponse{
		Status:                status,
		Phase:                 phase,
		Configured:            true,
		Enabled:               s.settings.Enabled,
		IntervalHours:         s.settings.IntervalHours,
		Scope:                 s.settings.Scope,
		KeepCount:             s.settings.KeepCount,
		KeepDays:              s.settings.KeepDays,
		ResticKeepLast:        s.settings.ResticKeepLast,
		ResticKeepDaily:       s.settings.ResticKeepDaily,
		IncludeACMEChallenges: s.settings.IncludeACMEChallenges,
		ArchiveDir:            s.archiveDir,
		Detail:                s.detail,
		ResticEnabled:         s.settings.ResticEnabled,
		ResticRepoSet:         s.resticRepo != "",
		ArchiveCount:          len(archives),
		RecentArchives:        archives,
	}
	if len(resp.RecentArchives) > recentArchivesMax {
		resp.RecentArchives = resp.RecentArchives[:recentArchivesMax]
	}
	if !s.lastBackup.IsZero() {
		resp.LastBackup = s.lastBackup.UTC().Format(time.RFC3339)
		next := s.lastBackup.Add(time.Duration(s.settings.IntervalHours) * time.Hour)
		resp.NextDue = next.UTC().Format(time.RFC3339)
	} else if s.settings.Enabled {
		resp.NextDue = time.Now().UTC().Format(time.RFC3339)
	}
	if s.job != nil {
		jobCopy := *s.job
		jobCopy.Steps = append([]jobStep(nil), s.job.Steps...)
		jobCopy.Log = append([]string(nil), s.job.Log...)
		resp.Job = &jobCopy
	}
	return resp
}
