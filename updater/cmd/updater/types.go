package main

import (
	"sync"
	"time"
)

type settings struct {
	IntervalHours int  `json:"interval_hours"`
	AutoUpdate    bool `json:"auto_update"`
	PinDigests    bool `json:"pin_digests"`
}

type serviceState struct {
	Name            string `json:"name"`
	Image           string `json:"image"`
	Digest          string `json:"digest,omitempty"`
	PinnedDigest    string `json:"pinned_digest,omitempty"`
	PinnedImage     string `json:"pinned_image,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	SignatureOK     *bool  `json:"signature_ok,omitempty"`
	SignatureDetail string `json:"signature_detail,omitempty"`
	Registry        string `json:"registry,omitempty"`
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

type statusResponse struct {
	Status        string         `json:"status"`
	Phase         string         `json:"phase,omitempty"`
	Available     bool           `json:"available"`
	LastCheck     string         `json:"last_check,omitempty"`
	LastApply     string         `json:"last_apply,omitempty"`
	IntervalHours int            `json:"interval_hours"`
	AutoUpdate    bool           `json:"auto_update"`
	PinDigests    bool           `json:"pin_digests"`
	Services      []serviceState `json:"services"`
	Detail        string         `json:"detail,omitempty"`
	Configured    bool           `json:"configured"`
	ImagePrefix   string         `json:"image_prefix,omitempty"`
	VerifyEnabled bool           `json:"verify_enabled"`
	Job           *jobState      `json:"job,omitempty"`
}

type pinsFile struct {
	Services map[string]string `json:"services"`
}

type imageInfo struct {
	ID       string
	Ref      string
	Digest   string
	Registry string
}

type server struct {
	token            string
	composeDir       string
	statePath        string
	pinsPath         string
	overridePath     string
	imagePrefix      string
	verifyEnabled    bool
	requireVerify    bool
	cosignIdentity   string
	cosignOIDCIssuer string
	cosignBin        string

	mu        sync.Mutex
	settings  settings
	lastCheck time.Time
	lastApply time.Time
	services  []serviceState
	available bool
	detail    string
	busy      bool
	phase     string
	job       *jobState
	pins      map[string]string
}
