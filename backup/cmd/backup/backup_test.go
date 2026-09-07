package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeSettings(t *testing.T) {
	got := normalizeSettings(settings{Scope: "nope", IntervalHours: 0, KeepCount: 0, KeepDays: 0})
	if got.Scope != "full" || got.IntervalHours != 1 || got.KeepCount != 1 || got.KeepDays != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestKeepNewest(t *testing.T) {
	names := []string{"snikketx-backup-b", "other", "snikketx-backup-a", "snikketx-backup-c"}
	got := keepNewest(names, 2)
	if len(got) != 2 || got[0] != "snikketx-backup-c" || got[1] != "snikketx-backup-b" {
		t.Fatalf("%v", got)
	}
}

func TestDryRunRestore(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "snikketx-backup-test")
	if err := os.MkdirAll(archive, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archive, "snikket-data-test.tar.gz"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archive, "portal-data-test.tar.gz"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &server{archiveDir: dir}
	res := s.dryRunRestore(archive)
	if !res.OK {
		t.Fatalf("%+v", res)
	}
}

func TestApplyRetentionKeepCount(t *testing.T) {
	dir := t.TempDir()
	s := &server{
		archiveDir: dir,
		settings:   settings{KeepCount: 2, KeepDays: 3650},
	}
	for _, name := range []string{"snikketx-backup-1", "snikketx-backup-2", "snikketx-backup-3"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(path, 0o750); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(path, "snikket-data-x.tar.gz"), []byte("x"), 0o600)
		past := time.Now().Add(-time.Hour)
		_ = os.Chtimes(path, past, past)
	}
	removed, err := s.applyRetention()
	if err != nil {
		t.Fatal(err)
	}
	if removed < 1 {
		t.Fatalf("expected removals, got %d", removed)
	}
	left := s.listArchives()
	if len(left) != 2 {
		t.Fatalf("left=%d %+v", len(left), left)
	}
}
