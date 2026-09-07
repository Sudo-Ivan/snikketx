package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type logService struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type logsResponse struct {
	Service   string       `json:"service"`
	Label     string       `json:"label"`
	Tail      int          `json:"tail"`
	Lines     []string     `json:"lines"`
	Truncated bool         `json:"truncated,omitempty"`
	Services  []logService `json:"services"`
}

var allowedLogServices = []logService{
	{ID: "snikket_server", Label: "Chat server (Prosody)"},
	{ID: "snikket_portal", Label: "Web portal"},
	{ID: "ravenguard", Label: "Edge (RavenGuard)"},
	{ID: "snikket_updater", Label: "Updater"},
	{ID: "snikket_backup", Label: "Backup"},
	{ID: "snikket_certs", Label: "Cert manager"},
}

func resolveLogService(id string) (logService, bool) {
	id = strings.TrimSpace(id)
	for _, svc := range allowedLogServices {
		if svc.ID == id {
			return svc, true
		}
	}
	return logService{}, false
}

func clampTail(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return 200
	}
	if n > 500 {
		return 500
	}
	return n
}

func (s *server) fetchComposeLogs(ctx context.Context, service string, tail int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "logs", "--no-color", "--tail", strconv.Itoa(tail), service)
	cmd.Dir = s.composeDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", truncate(msg, 400))
	}
	return string(out), nil
}

func splitLogLines(raw string, maxBytes int) ([]string, bool) {
	truncated := false
	if len(raw) > maxBytes {
		raw = raw[len(raw)-maxBytes:]
		if i := strings.IndexByte(raw, '\n'); i >= 0 && i+1 < len(raw) {
			raw = raw[i+1:]
		}
		truncated = true
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return []string{}, truncated
	}
	return strings.Split(raw, "\n"), truncated
}
