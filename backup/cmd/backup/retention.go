package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *server) applyRetention() (int, error) {
	s.mu.Lock()
	keepCount := s.settings.KeepCount
	keepDays := s.settings.KeepDays
	s.mu.Unlock()

	archives := s.listArchives()
	if len(archives) == 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().Add(-time.Duration(keepDays) * 24 * time.Hour)
	removed := 0

	// Drop by age first.
	remaining := make([]archiveInfo, 0, len(archives))
	for _, a := range archives {
		created := time.Time{}
		if a.Created != "" {
			created, _ = time.Parse(time.RFC3339, a.Created)
		}
		if !created.IsZero() && created.Before(cutoff) && len(archives)-removed > keepCount {
			if err := os.RemoveAll(a.Path); err != nil {
				return removed, err
			}
			removed++
			continue
		}
		remaining = append(remaining, a)
	}

	// Newest first already from listArchives. Keep only keepCount.
	sort.Slice(remaining, func(i, j int) bool { return remaining[i].Name > remaining[j].Name })
	for i := keepCount; i < len(remaining); i++ {
		if err := os.RemoveAll(remaining[i].Path); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// keepNewest is used by tests to assert retention helpers stay stable.
func keepNewest(names []string, n int) []string {
	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(filepath.Base(name), "snikketx-backup-") {
			filtered = append(filtered, name)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i] > filtered[j] })
	if n >= len(filtered) {
		return filtered
	}
	return filtered[:n]
}
