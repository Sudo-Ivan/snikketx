package main

import (
	"context"
	"os/exec"
	"strings"
)

func (s *server) verifyImage(ctx context.Context, img imageInfo) (bool, string) {
	target := img.Ref
	if img.Digest != "" && !strings.Contains(target, "@") {
		base := strings.Split(target, ":")[0]
		target = base + "@" + img.Digest
	}
	if target == "" {
		return false, "missing image reference"
	}
	args := []string{
		"verify",
		"--certificate-identity-regexp", s.cosignIdentity,
		"--certificate-oidc-issuer", s.cosignOIDCIssuer,
		target,
	}
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
	args := []string{
		"verify-attestation",
		"--type", "spdxjson",
		"--certificate-identity-regexp", s.cosignIdentity,
		"--certificate-oidc-issuer", s.cosignOIDCIssuer,
		target,
	}
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
