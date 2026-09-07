package main

import (
	"sync"
	"time"
)

type settings struct {
	Enabled               bool   `json:"enabled"`
	IntervalHours         int    `json:"interval_hours"`
	Scope                 string `json:"scope"`
	KeepCount             int    `json:"keep_count"`
	KeepDays              int    `json:"keep_days"`
	ResticEnabled         bool   `json:"restic_enabled"`
	ResticKeepLast        int    `json:"restic_keep_last"`
	ResticKeepDaily       int    `json:"restic_keep_daily"`
	IncludeACMEChallenges bool   `json:"include_acme_challenges"`
}

type jobStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	At     string `json:"at,omitempty"`
}

type jobState struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Status  string    `json:"status"`
	Phase   string    `json:"phase"`
	Steps   []jobStep `json:"steps"`
	Log     []string  `json:"log"`
	Error   string    `json:"error,omitempty"`
	Started string    `json:"started,omitempty"`
	Ended   string    `json:"ended,omitempty"`
}

type archiveInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Created   string `json:"created,omitempty"`
	HasData   bool   `json:"has_data"`
	HasFull   bool   `json:"has_full"`
}

type dryRunResult struct {
	OK       bool     `json:"ok"`
	Archive  string   `json:"archive"`
	Messages []string `json:"messages"`
	Files    []string `json:"files,omitempty"`
}

type statusResponse struct {
	Status                string        `json:"status"`
	Phase                 string        `json:"phase,omitempty"`
	Configured            bool          `json:"configured"`
	Enabled               bool          `json:"enabled"`
	IntervalHours         int           `json:"interval_hours"`
	Scope                 string        `json:"scope"`
	KeepCount             int           `json:"keep_count"`
	KeepDays              int           `json:"keep_days"`
	ResticKeepLast        int           `json:"restic_keep_last"`
	ResticKeepDaily       int           `json:"restic_keep_daily"`
	IncludeACMEChallenges bool          `json:"include_acme_challenges"`
	ArchiveDir            string        `json:"archive_dir"`
	LastBackup            string        `json:"last_backup,omitempty"`
	NextDue               string        `json:"next_due,omitempty"`
	Detail                string        `json:"detail,omitempty"`
	ResticEnabled         bool          `json:"restic_enabled"`
	ResticRepoSet         bool          `json:"restic_repo_set"`
	ArchiveCount          int           `json:"archive_count"`
	Job                   *jobState     `json:"job,omitempty"`
	RecentArchives        []archiveInfo `json:"recent_archives,omitempty"`
}

type server struct {
	token        string
	tokenBytes   []byte
	composeDir   string
	archiveDir   string
	statePath    string
	settingsPath string
	alpineImage  string
	resticBin    string
	resticRepo   string
	resticPass   string

	mu         sync.Mutex
	settings   settings
	lastBackup time.Time
	busy       bool
	phase      string
	detail     string
	job        *jobState
}
