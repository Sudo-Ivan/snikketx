package main

import (
	"context"
	"os/exec"
	"strings"
)

// validImageRef rejects empty refs, whitespace and flag injection.
func validImageRef(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return false
	}
	return !strings.ContainsAny(ref, " \t\n\r")
}

func (s *server) verifyImage(ctx context.Context, img imageInfo) (bool, string) {
	target := img.Ref
	if img.Digest != "" && !strings.Contains(target, "@") {
		base := imageRefBase(target)
		target = base + "@" + img.Digest
	}
	if !validImageRef(target) {
		return false, "missing or invalid image reference"
	}
	args := []string{
		"verify",
		"--certificate-identity-regexp", s.cosignIdentity,
		"--certificate-oidc-issuer", s.cosignOIDCIssuer,
		target,
	}
	// #nosec G204 -- cosignBin is operator-configured, target validated above
	cmd := exec.CommandContext(ctx, s.cosignBin, args...)
	out, err := cmd.CombinedOutput()
	detail := strings.TrimSpace(string(out))
	if err != nil {
		if detail == "" {
			detail = err.Error()
		}
		return false, truncate(detail, 240)
	}
	attOK, attDetail := s.verifyAttestation(ctx, target)
	if !attOK {
		return true, "signature ok; sbom attestation: " + truncate(attDetail, 120)
	}
	return true, "signature and sbom attestation verified"
}

func (s *server) verifyAttestation(ctx context.Context, target string) (bool, string) {
	if !validImageRef(target) {
		return false, "missing or invalid image reference"
	}
	args := []string{
		"verify-attestation",
		"--type", "spdxjson",
		"--certificate-identity-regexp", s.cosignIdentity,
		"--certificate-oidc-issuer", s.cosignOIDCIssuer,
		target,
	}
	// #nosec G204 -- cosignBin is operator-configured, target validated above
	cmd := exec.CommandContext(ctx, s.cosignBin, args...)
	out, err := cmd.CombinedOutput()
	detail := strings.TrimSpace(string(out))
	if err != nil {
		if detail == "" {
			detail = err.Error()
		}
		return false, detail
	}
	return true, "ok"
}
