package main

import (
	"strings"
	"testing"
)

func TestResolveLogService(t *testing.T) {
	svc, ok := resolveLogService("snikket_server")
	if !ok || svc.Label == "" {
		t.Fatalf("expected snikket_server")
	}
	if _, ok := resolveLogService("../etc/passwd"); ok {
		t.Fatal("path traversal must be rejected")
	}
	if _, ok := resolveLogService("not-a-service"); ok {
		t.Fatal("unknown service must be rejected")
	}
}

func TestClampTail(t *testing.T) {
	if got := clampTail(""); got != 200 {
		t.Fatalf("default=%d", got)
	}
	if got := clampTail("50"); got != 50 {
		t.Fatalf("50=%d", got)
	}
	if got := clampTail("9999"); got != 500 {
		t.Fatalf("cap=%d", got)
	}
}

func TestSplitLogLines(t *testing.T) {
	lines, truncated := splitLogLines("a\nb\nc\n", 1024)
	if truncated || len(lines) != 3 {
		t.Fatalf("lines=%v truncated=%v", lines, truncated)
	}
	big := stringsRepeat("x", 100) + "\n" + stringsRepeat("y", 100)
	lines, truncated = splitLogLines(big, 120)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if len(lines) == 0 {
		t.Fatal("expected remaining lines")
	}
}

func TestParseComposeLogLine(t *testing.T) {
	entry := parseComposeLogLine("snikket  | 2026-09-07T11:33:33.406097562Z Waiting for certificates")
	if entry.Display != "11:33:33" {
		t.Fatalf("display=%q", entry.Display)
	}
	if entry.Text != "Waiting for certificates" {
		t.Fatalf("text=%q", entry.Text)
	}
	if entry.Time != "2026-09-07T11:33:33Z" {
		t.Fatalf("time=%q", entry.Time)
	}
	wide := parseComposeLogLine("snikket  | 2026-09-07T11:33:33Z -c          <filename>          Configuration file")
	if wide.Text != "-c <filename> Configuration file" {
		t.Fatalf("compact text=%q", wide.Text)
	}
	plain := parseComposeLogLine("not a compose line")
	if plain.Text != "not a compose line" || plain.Display != "" {
		t.Fatalf("plain=%+v", plain)
	}
}

func TestBuildLogEntries(t *testing.T) {
	entries, lines := buildLogEntries([]string{
		"snikket  | 2026-09-07T11:33:33.406097562Z hello",
	})
	if len(entries) != 1 || len(lines) != 1 {
		t.Fatalf("len entries=%d lines=%d", len(entries), len(lines))
	}
	if !strings.Contains(lines[0], "11:33:33") || !strings.Contains(lines[0], "hello") {
		t.Fatalf("line=%q", lines[0])
	}
}

func BenchmarkParseComposeLogLine(b *testing.B) {
	line := "snikket  | 2026-09-07T11:33:33.406097562Z Waiting for certificates to become available..."
	b.ReportAllocs()
	for b.Loop() {
		_ = parseComposeLogLine(line)
	}
}

func BenchmarkCompactLogTextHot(b *testing.B) {
	line := "Waiting for certificates to become available..."
	b.ReportAllocs()
	for b.Loop() {
		_ = compactLogText(line)
	}
}

func TestImageRefBase(t *testing.T) {
	cases := map[string]string{
		"ghcr.io/sudo-ivan/snikketx/server:latest":     "ghcr.io/sudo-ivan/snikketx/server",
		"ghcr.io/sudo-ivan/snikketx/server@sha256:abc": "ghcr.io/sudo-ivan/snikketx/server",
		"localhost:5000/snikketx/server:dev":           "localhost:5000/snikketx/server",
		"alpine:3.24":                                  "alpine",
	}
	for in, want := range cases {
		if got := imageRefBase(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

func BenchmarkSplitLogLines(b *testing.B) {
	var raw strings.Builder
	for i := 0; i < 200; i++ {
		raw.WriteString("snikket  | 2026-09-07T11:33:33.406097562Z line ")
		raw.WriteByte(byte('0' + i%10))
		raw.WriteByte('\n')
	}
	s := raw.String()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = splitLogLines(s, 512<<10)
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
