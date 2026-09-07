package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (s *server) resticEnv() []string {
	env := os.Environ()
	if s.resticRepo != "" {
		env = append(env, "RESTIC_REPOSITORY="+s.resticRepo)
	}
	if s.resticPass != "" {
		env = append(env, "RESTIC_PASSWORD="+s.resticPass)
	}
	return env
}

func (s *server) resticCheck(ctx context.Context) (string, error) {
	if s.resticRepo == "" {
		return "", fmt.Errorf("RESTIC_REPOSITORY is not set")
	}
	cmd := exec.CommandContext(ctx, s.resticBin, "snapshots", "--json")
	cmd.Env = s.resticEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (s *server) resticBackup(ctx context.Context, archiveDir string) error {
	if s.resticRepo == "" {
		return fmt.Errorf("RESTIC_REPOSITORY is not set")
	}
	if s.resticPass == "" {
		return fmt.Errorf("RESTIC_PASSWORD or RESTIC_PASSWORD_FILE is required")
	}
	// Ensure repo exists.
	initCmd := exec.CommandContext(ctx, s.resticBin, "init")
	initCmd.Env = s.resticEnv()
	_ = initCmd.Run() // ignore already initialized

	cmd := exec.CommandContext(ctx, s.resticBin, "backup", archiveDir)
	cmd.Env = s.resticEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic backup: %s", strings.TrimSpace(string(out)))
	}

	s.mu.Lock()
	keepLast := s.settings.ResticKeepLast
	keepDaily := s.settings.ResticKeepDaily
	s.mu.Unlock()
	args := []string{"forget", "--prune", "--keep-last", fmt.Sprintf("%d", keepLast)}
	if keepDaily > 0 {
		args = append(args, "--keep-daily", fmt.Sprintf("%d", keepDaily))
	}
	forget := exec.CommandContext(ctx, s.resticBin, args...)
	forget.Env = s.resticEnv()
	out, err = forget.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic forget: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
