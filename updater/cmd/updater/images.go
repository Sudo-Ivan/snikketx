package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
)

func (s *server) buildServiceStates(ctx context.Context, before, after map[string]imageInfo) ([]serviceState, bool, int) {
	s.mu.Lock()
	pins := copyMap(s.pins)
	s.mu.Unlock()

	services := make([]serviceState, 0, len(after))
	available := false
	verifyFailed := 0
	names := make([]string, 0, len(after))
	for name := range after {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		img := after[name]
		prev := before[name]
		changed := prev.ID != "" && prev.ID != img.ID
		pinned := pins[name]
		if pinned != "" && img.Digest != "" && !strings.Contains(pinned, img.Digest) {
			changed = true
		}
		if changed {
			available = true
		}
		row := serviceState{
			Name:            name,
			Image:           img.Ref,
			Digest:          img.Digest,
			UpdateAvailable: changed,
			Registry:        img.Registry,
			PinnedImage:     pinned,
		}
		if pinned != "" {
			if at := strings.Index(pinned, "@"); at >= 0 {
				row.PinnedDigest = pinned[at+1:]
			}
		}
		if s.verifyEnabled && strings.HasPrefix(img.Ref, s.imagePrefix+"/") {
			ok, detail := s.verifyImage(ctx, img)
			row.SignatureOK = &ok
			row.SignatureDetail = detail
			s.logf("verify %s: %v", name, ok)
			if !ok {
				verifyFailed++
			}
		}
		services = append(services, row)
	}
	return services, available, verifyFailed
}

func (s *server) compose(ctx context.Context, args ...string) (string, error) {
	cmdArgs := append([]string{"compose"}, args...)
	s.mu.Lock()
	pinMode := s.settings.PinDigests
	hasPins := len(s.pins) > 0
	s.mu.Unlock()
	if pinMode && hasPins && fileExists(s.overridePath) && (len(args) > 0 && (args[0] == "up" || args[0] == "images")) {
		cmdArgs = append([]string{"compose", "-f", "docker-compose.yml", "-f", "docker-compose.pins.yml"}, args...)
	}
	// #nosec G204 -- args are internal compose subcommands (pull/up/images)
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	cmd.Dir = s.composeDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (s *server) imageInventory(ctx context.Context) (map[string]imageInfo, error) {
	out, err := s.compose(ctx, "images", "--format", "json")
	if err != nil {
		return map[string]imageInfo{}, nil
	}
	result := map[string]imageInfo{}
	dec := json.NewDecoder(strings.NewReader(out))
	for {
		var row map[string]any
		if err := dec.Decode(&row); err != nil {
			break
		}
		name, _ := row["Service"].(string)
		if name == "" {
			name, _ = row["ContainerName"].(string)
		}
		id, _ := row["ID"].(string)
		repo, _ := row["Repository"].(string)
		tag, _ := row["Tag"].(string)
		ref := strings.TrimSpace(repo)
		if ref != "" && tag != "" && tag != "<none>" {
			ref = ref + ":" + tag
		}
		if ref == "" {
			ref = id
		}
		digest := ""
		if id != "" {
			digest = s.imageDigest(ctx, id)
			if digest != "" && !strings.Contains(ref, "@") {
				base := repo
				if base == "" {
					base = imageRefBase(ref)
				}
				ref = base + "@" + digest
			}
		}
		registry := ""
		if i := strings.IndexByte(ref, '/'); i > 0 {
			registry = ref[:i]
		}
		if name != "" && (id != "" || ref != "") {
			result[name] = imageInfo{ID: id, Ref: ref, Digest: digest, Registry: registry}
		}
	}
	return result, nil
}

// validImageID accepts docker image ids: hex digest with optional algo prefix.
func validImageID(id string) bool {
	if i := strings.IndexByte(id, ':'); i >= 0 {
		id = id[i+1:]
	}
	if len(id) < 12 || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *server) imageDigest(ctx context.Context, id string) string {
	if !validImageID(id) {
		return ""
	}
	// #nosec G204 -- id validated as a hex image id above
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{index .RepoDigests 0}}", id)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if i := strings.LastIndex(line, "@"); i >= 0 {
		return line[i+1:]
	}
	return ""
}

// imageRefBase strips tag and digest suffixes from an image reference.
func imageRefBase(ref string) string {
	if i := strings.IndexByte(ref, '@'); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndexByte(ref, ':'); i >= 0 {
		// Keep registry ports like host:5000/name by only stripping a tag after the last slash.
		if j := strings.LastIndexByte(ref, '/'); j < i {
			ref = ref[:i]
		}
	}
	return ref
}
