package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *server) loop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		interval := time.Duration(s.settings.IntervalHours) * time.Hour
		auto := s.settings.AutoUpdate
		due := s.lastCheck.IsZero() || time.Since(s.lastCheck) >= interval
		busy := s.busy
		s.mu.Unlock()
		if !due || busy {
			continue
		}
		_ = s.startJob("check", func(ctx context.Context) error {
			if err := s.runCheckJob(ctx); err != nil {
				return err
			}
			if auto {
				s.mu.Lock()
				available := s.available
				s.mu.Unlock()
				if available {
					return s.runApplyJob(ctx)
				}
			}
			return nil
		})
	}
}

func (s *server) startJob(kind string, fn func(context.Context) error) error {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return errors.New("updater is busy")
	}
	s.busy = true
	s.phase = kind + "ing"
	if kind == "apply" {
		s.phase = "applying"
	}
	if kind == "check" {
		s.phase = "checking"
	}
	job := &jobState{
		ID:      fmt.Sprintf("%s-%d", kind, time.Now().UTC().Unix()),
		Kind:    kind,
		Status:  "running",
		Phase:   s.phase,
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

func (s *server) runCheckJob(ctx context.Context) error {
	s.setStep("inventory", "running", "reading compose images")
	s.logf("inventory: reading local compose images")
	before, err := s.imageInventory(ctx)
	if err != nil {
		s.setStep("inventory", "failed", err.Error())
		return err
	}
	s.setStep("inventory", "done", fmt.Sprintf("%d services", len(before)))

	s.setStep("pull", "running", "docker compose pull")
	s.logf("pull: starting docker compose pull")
	if out, err := s.compose(ctx, "pull"); err != nil {
		s.setStep("pull", "failed", strings.TrimSpace(out))
		s.logf("pull failed: %s", strings.TrimSpace(out))
		return err
	}
	s.setStep("pull", "done", "images pulled")
	s.logf("pull: complete")

	s.setStep("inventory", "running", "re-reading images")
	after, err := s.imageInventory(ctx)
	if err != nil {
		s.setStep("inventory", "failed", err.Error())
		return err
	}

	s.setStep("verify", "running", "cosign keyless verification")
	services, available, verifyFailed := s.buildServiceStates(ctx, before, after)
	if verifyFailed > 0 {
		s.setStep("verify", "failed", fmt.Sprintf("%d signature check(s) failed", verifyFailed))
		s.logf("verify: %d failed", verifyFailed)
		if s.requireVerify {
			s.mu.Lock()
			s.services = services
			s.available = false
			s.lastCheck = time.Now().UTC()
			s.mu.Unlock()
			return fmt.Errorf("signature verification failed for %d image(s)", verifyFailed)
		}
	} else {
		s.setStep("verify", "done", "signatures checked")
		s.logf("verify: complete")
	}

	detail := "all tracked images are current"
	if available {
		detail = "newer container images are ready"
	}
	s.mu.Lock()
	s.services = services
	s.available = available
	s.lastCheck = time.Now().UTC()
	s.detail = detail
	s.mu.Unlock()
	s.logf("check finished: %s", detail)
	return nil
}

func (s *server) runApplyJob(ctx context.Context) error {
	s.mu.Lock()
	pinMode := s.settings.PinDigests
	pins := copyMap(s.pins)
	services := append([]serviceState(nil), s.services...)
	s.mu.Unlock()

	if s.requireVerify {
		for _, svc := range services {
			if svc.SignatureOK != nil && !*svc.SignatureOK {
				return fmt.Errorf("refusing to apply unsigned or unverified image %s", svc.Name)
			}
		}
	}

	s.setStep("apply", "running", "docker compose up -d")
	s.logf("apply: starting compose up")
	if pinMode && len(pins) > 0 {
		if err := s.writePinOverride(pins); err != nil {
			s.setStep("apply", "failed", err.Error())
			return err
		}
		s.logf("apply: using pinned digests for %d services", len(pins))
	}
	if out, err := s.compose(ctx, "up", "-d"); err != nil {
		s.setStep("apply", "failed", strings.TrimSpace(out))
		s.logf("apply failed: %s", strings.TrimSpace(out))
		return err
	}
	s.setStep("apply", "done", "containers updated")
	s.logf("apply: containers updated")

	s.setStep("pin", "running", "recording digests")
	after, err := s.imageInventory(ctx)
	if err != nil {
		s.setStep("pin", "failed", err.Error())
		return err
	}
	if pinMode {
		nextPins := map[string]string{}
		for name, img := range after {
			if img.Digest == "" || !strings.HasPrefix(img.Ref, s.imagePrefix+"/") {
				continue
			}
			base := imageRefBase(img.Ref)
			nextPins[name] = base + "@" + img.Digest
		}
		s.mu.Lock()
		s.pins = nextPins
		s.mu.Unlock()
		_ = s.savePins()
		_ = s.writePinOverride(nextPins)
		s.logf("pin: locked %d digests", len(nextPins))
	}
	s.setStep("pin", "done", "digest pins saved")

	s.mu.Lock()
	s.lastApply = time.Now().UTC()
	s.available = false
	for i := range s.services {
		s.services[i].UpdateAvailable = false
		if ref, ok := s.pins[s.services[i].Name]; ok {
			s.services[i].PinnedImage = ref
			if at := strings.Index(ref, "@"); at >= 0 {
				s.services[i].PinnedDigest = ref[at+1:]
			}
		}
	}
	s.detail = "containers updated"
	s.mu.Unlock()
	return nil
}
