package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (s *server) loop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		enabled := s.settings.Enabled
		interval := time.Duration(s.settings.IntervalHours) * time.Hour
		due := s.lastBackup.IsZero() || time.Since(s.lastBackup) >= interval
		busy := s.busy
		s.mu.Unlock()
		if !enabled || !due || busy {
			continue
		}
		_ = s.startJob("backup", s.runBackupJob)
	}
}

func (s *server) startJob(kind string, fn func(context.Context) error) error {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return errors.New("backup service is busy")
	}
	s.busy = true
	s.phase = "running"
	job := &jobState{
		ID:      fmt.Sprintf("%s-%d", kind, time.Now().UTC().Unix()),
		Kind:    kind,
		Status:  "running",
		Phase:   "running",
		Steps:   []jobStep{},
		Log:     []string{},
		Started: time.Now().UTC().Format(time.RFC3339),
	}
	s.job = job
	s.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
		defer cancel()
		err := fn(ctx)
		s.mu.Lock()
		s.busy = false
		s.phase = "idle"
		if s.job != nil {
			s.job.Ended = time.Now().UTC().Format(time.RFC3339)
			if err != nil {
				s.job.Status = "failed"
				s.job.Error = err.Error()
				s.job.Phase = "failed"
				s.detail = err.Error()
			} else {
				s.job.Status = "succeeded"
				s.job.Phase = "idle"
			}
		}
		s.mu.Unlock()
	}()
	return nil
}

func (s *server) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil {
		s.job.Log = append(s.job.Log, time.Now().UTC().Format(time.RFC3339)+" "+line)
		if len(s.job.Log) > jobLogMaxLines {
			s.job.Log = s.job.Log[len(s.job.Log)-jobLogMaxLines:]
		}
	}
	s.detail = line
}

func (s *server) setStep(name, status, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job == nil {
		return
	}
	at := time.Now().UTC().Format(time.RFC3339)
	for i := range s.job.Steps {
		if s.job.Steps[i].Name == name {
			s.job.Steps[i].Status = status
			s.job.Steps[i].Detail = detail
			s.job.Steps[i].At = at
			return
		}
	}
	s.job.Steps = append(s.job.Steps, jobStep{Name: name, Status: status, Detail: detail, At: at})
}

func (s *server) runBackupJob(ctx context.Context) error {
	s.setStep("backup", "running", "creating archive")
	s.logf("backup: starting")
	outDir, err := s.createBackup(ctx)
	if err != nil {
		s.setStep("backup", "failed", err.Error())
		return err
	}
	s.setStep("backup", "done", outDir)
	s.logf("backup: wrote %s", outDir)

	s.setStep("retain", "running", "applying retention")
	removed, err := s.applyRetention()
	if err != nil {
		s.setStep("retain", "failed", err.Error())
		return err
	}
	s.setStep("retain", "done", fmt.Sprintf("removed %d", removed))
	s.logf("retention: removed %d archives", removed)

	s.mu.Lock()
	resticOn := s.settings.ResticEnabled
	s.mu.Unlock()
	if resticOn {
		s.setStep("restic", "running", "pushing offsite")
		if err := s.resticBackup(ctx, outDir); err != nil {
			s.setStep("restic", "failed", err.Error())
			return err
		}
		s.setStep("restic", "done", "offsite ok")
		s.logf("restic: push complete")
	}

	s.mu.Lock()
	s.lastBackup = time.Now().UTC()
	s.detail = "backup complete"
	s.mu.Unlock()
	_ = s.saveState()
	return nil
}
