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

type logEntry struct {
	Time    string `json:"time,omitempty"`
	Display string `json:"display,omitempty"`
	Text    string `json:"text"`
}

type logsResponse struct {
	Service   string       `json:"service"`
	Label     string       `json:"label"`
	Tail      int          `json:"tail"`
	Lines     []string     `json:"lines"`
	Entries   []logEntry   `json:"entries"`
	Truncated bool         `json:"truncated,omitempty"`
	FetchedAt string       `json:"fetched_at,omitempty"`
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
	cmd := exec.CommandContext(ctx, "docker", "compose", "logs", "--no-color", "--timestamps", "--tail", strconv.Itoa(tail), service)
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
	if strings.IndexByte(raw, '\r') >= 0 {
		raw = strings.ReplaceAll(raw, "\r\n", "\n")
		raw = strings.ReplaceAll(raw, "\r", "\n")
	}
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return []string{}, truncated
	}
	n := 1 + strings.Count(raw, "\n")
	out := make([]string, 0, n)
	for {
		i := strings.IndexByte(raw, '\n')
		if i < 0 {
			out = append(out, raw)
			return out, truncated
		}
		out = append(out, raw[:i])
		raw = raw[i+1:]
	}
}

// parseComposeLogLine parses docker compose logs --timestamps output without a regexp.
func parseComposeLogLine(line string) logEntry {
	pipe := strings.Index(line, " | ")
	if pipe < 0 {
		return logEntry{Text: compactLogText(line)}
	}
	rest := line[pipe+3:]
	sp := strings.IndexByte(rest, ' ')
	if sp < 20 {
		return logEntry{Text: compactLogText(line)}
	}
	tsRaw := rest[:sp]
	if !looksLikeComposeTimestamp(tsRaw) {
		return logEntry{Text: compactLogText(line)}
	}
	msg := compactLogText(rest[sp+1:])
	ts, err := time.Parse(time.RFC3339Nano, tsRaw)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, tsRaw)
	}
	if err != nil {
		return logEntry{Time: tsRaw, Display: tsRaw, Text: msg}
	}
	utc := ts.UTC()
	return logEntry{
		Time:    utc.Format(time.RFC3339),
		Display: utc.Format("15:04:05"),
		Text:    msg,
	}
}

func looksLikeComposeTimestamp(s string) bool {
	// YYYY-MM-DDTHH:MM:SS[.fffffffff]Z
	n := len(s)
	if n < 20 || n > 40 {
		return false
	}
	if s[n-1] != 'Z' || s[4] != '-' || s[7] != '-' || s[10] != 'T' || s[13] != ':' || s[16] != ':' {
		return false
	}
	return true
}

func compactLogText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\t' || c == '\n' || c == '\r' {
			return strings.Join(strings.Fields(s), " ")
		}
		if c == ' ' && i+1 < len(s) && s[i+1] == ' ' {
			return strings.Join(strings.Fields(s), " ")
		}
	}
	return s
}

func buildLogEntries(rawLines []string) ([]logEntry, []string) {
	entries := make([]logEntry, len(rawLines))
	lines := make([]string, len(rawLines))
	for i, raw := range rawLines {
		entry := parseComposeLogLine(raw)
		entries[i] = entry
		if entry.Display != "" {
			lines[i] = entry.Display + " " + entry.Text
		} else {
			lines[i] = entry.Text
		}
	}
	return entries, lines
}
