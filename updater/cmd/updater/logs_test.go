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

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
