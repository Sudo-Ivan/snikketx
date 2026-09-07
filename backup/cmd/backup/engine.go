package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *server) createBackup(ctx context.Context) (string, error) {
	s.mu.Lock()
	scope := s.settings.Scope
	includeACME := s.settings.IncludeACMEChallenges
	s.mu.Unlock()

	if err := s.requireSnikket(); err != nil {
		return "", err
	}

	stamp := time.Now().UTC().Format("2006-01-02-150405")
	out := filepath.Join(s.archiveDir, "snikketx-backup-"+stamp)
	if err := os.MkdirAll(out, 0o750); err != nil {
		return "", err
	}

	dataName := "snikket-data-" + stamp + ".tar.gz"
	s.logf("archiving /snikket")
	if err := s.dockerRun(ctx,
		"--volumes-from=snikket",
		"-v", out+":/backup",
		s.alpineImage,
		"tar", "czf", "/backup/"+dataName, "/snikket",
	); err != nil {
		return "", fmt.Errorf("prosody data: %w", err)
	}

	_ = s.copyIfExists(filepath.Join(s.composeDir, "snikket.conf"), filepath.Join(out, "snikket.conf"))
	_ = s.copyIfExists(filepath.Join(s.composeDir, ".env"), filepath.Join(out, "env"))

	if scope == "full" {
		vols := []struct{ vol, label string }{
			{"snikketx_portal_data", "portal-data"},
			{"snikketx_ravenguard_data", "ravenguard-data"},
			{"snikketx_updater_data", "updater-data"},
			{"snikketx_backup_data", "backup-data"},
			{"snikket_portal_data", "portal-data-classic"},
		}
		if includeACME {
			vols = append(vols, struct{ vol, label string }{"snikketx_acme_challenges", "acme-challenges"})
		}
		for _, v := range vols {
			if !s.volumeExists(v.vol) {
				continue
			}
			s.logf("archiving volume %s", v.vol)
			if err := s.dockerRun(ctx,
				"-v", v.vol+":/data:ro",
				"-v", out+":/backup",
				s.alpineImage,
				"tar", "czf", "/backup/"+v.label+"-"+stamp+".tar.gz", "-C", "/", "data",
			); err != nil {
				return "", fmt.Errorf("volume %s: %w", v.vol, err)
			}
		}
	}

	mode := "classic"
	if s.volumeExists("snikketx_portal_data") || s.volumeExists("snikketx_ravenguard_data") {
		mode = "snikketx"
	}
	entries, _ := os.ReadDir(out)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	manifest := fmt.Sprintf("SnikketX backup %s\nmode=%s\nfull=%v\ncreated=%s\ncontents:\n%s\n",
		stamp, mode, scope == "full", time.Now().UTC().Format(time.RFC3339), strings.Join(names, "\n"))
	if err := os.WriteFile(filepath.Join(out, "MANIFEST.txt"), []byte(manifest), 0o600); err != nil {
		return "", err
	}
	return out, nil
}

func (s *server) requireSnikket() error {
	cmd := exec.Command("docker", "container", "inspect", "snikket")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("container snikket not found")
	}
	return nil
}

func (s *server) volumeExists(name string) bool {
	cmd := exec.Command("docker", "volume", "inspect", name)
	return cmd.Run() == nil
}

func (s *server) dockerRun(ctx context.Context, args ...string) error {
	cmdArgs := append([]string{"run", "--rm"}, args...)
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (s *server) copyIfExists(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

func (s *server) listArchives() []archiveInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listArchivesLocked()
}

func (s *server) listArchivesLocked() []archiveInfo {
	entries, err := os.ReadDir(s.archiveDir)
	if err != nil {
		return nil
	}
	out := make([]archiveInfo, 0)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "snikketx-backup-") {
			continue
		}
		path := filepath.Join(s.archiveDir, e.Name())
		info := archiveInfo{Name: e.Name(), Path: path}
		if st, err := e.Info(); err == nil {
			info.Created = st.ModTime().UTC().Format(time.RFC3339)
		}
		info.SizeBytes = dirSize(path)
		info.HasData = hasPrefixFile(path, "snikket-data-")
		info.HasFull = hasPrefixFile(path, "portal-data-") || hasPrefixFile(path, "ravenguard-data-")
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out
}

func dirSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

func hasPrefixFile(dir, prefix string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			return true
		}
	}
	return false
}

func (s *server) dryRunRestore(archiveDir string) dryRunResult {
	res := dryRunResult{Archive: archiveDir, Messages: []string{}}
	st, err := os.Stat(archiveDir)
	if err != nil || !st.IsDir() {
		res.Messages = append(res.Messages, "archive directory not found")
		return res
	}
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		res.Messages = append(res.Messages, err.Error())
		return res
	}
	for _, e := range entries {
		res.Files = append(res.Files, e.Name())
	}
	sort.Strings(res.Files)
	if !hasPrefixFile(archiveDir, "snikket-data-") {
		res.Messages = append(res.Messages, "missing snikket-data-*.tar.gz")
		return res
	}
	res.Messages = append(res.Messages, "Prosody data archive present")
	if hasPrefixFile(archiveDir, "portal-data-") {
		res.Messages = append(res.Messages, "portal-data archive present (use --full to restore)")
	}
	if hasPrefixFile(archiveDir, "ravenguard-data-") {
		res.Messages = append(res.Messages, "ravenguard-data archive present (use --full to restore)")
	}
	if hasPrefixFile(archiveDir, "updater-data-") {
		res.Messages = append(res.Messages, "updater-data archive present (use --full to restore)")
	}
	res.Messages = append(res.Messages, "dry-run only: no volumes were modified")
	res.OK = true
	return res
}

type persistedState struct {
	LastBackup string `json:"last_backup"`
}

func (s *server) loadState() {
	// #nosec G304 -- state path under controlled directory
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		return
	}
	var st persistedState
	if json.Unmarshal(data, &st) != nil || st.LastBackup == "" {
		return
	}
	if t, err := time.Parse(time.RFC3339, st.LastBackup); err == nil {
		s.lastBackup = t
	}
}

func (s *server) saveState() error {
	s.mu.Lock()
	st := persistedState{}
	if !s.lastBackup.IsZero() {
		st.LastBackup = s.lastBackup.UTC().Format(time.RFC3339)
	}
	s.mu.Unlock()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.statePath)
}

func (s *server) loadSettings() {
	// #nosec G304 -- settings path under controlled directory
	data, err := os.ReadFile(s.settingsPath)
	if err != nil {
		_ = s.saveSettings(s.settings)
		return
	}
	var next settings
	if json.Unmarshal(data, &next) != nil {
		return
	}
	s.mu.Lock()
	s.settings = normalizeSettings(next)
	s.mu.Unlock()
}

func (s *server) saveSettings(next settings) error {
	next = normalizeSettings(next)
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.settingsPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.settingsPath); err != nil {
		return err
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	return nil
}
