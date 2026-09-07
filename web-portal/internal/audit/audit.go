// Package audit keeps a searchable log of administrator actions taken through
// the web portal, with an optional feed of Prosody audit events.
package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultRing = 512
	maxLine     = 64 << 10
	maxUALen    = 240
)

// Event is one recorded action.
type Event struct {
	ID        string            `json:"id"`
	When      time.Time         `json:"when"`
	Source    string            `json:"source"`
	Actor     string            `json:"actor,omitempty"`
	Action    string            `json:"action"`
	Target    string            `json:"target,omitempty"`
	Detail    string            `json:"detail,omitempty"`
	IP        string            `json:"ip,omitempty"`
	UserAgent string            `json:"user_agent,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// Store appends events to a JSONL file and keeps a recent ring in memory.
type Store struct {
	mu   sync.Mutex
	path string
	ring []Event
	size int
	idx  int
	n    int
}

// Open creates or opens an append-only audit log under dir.
func Open(dir string, ringSize int) (*Store, error) {
	if ringSize < 1 {
		ringSize = defaultRing
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "audit.jsonl")
	s := &Store{
		path: path,
		ring: make([]Event, ringSize),
		size: ringSize,
	}
	if err := s.loadTail(); err != nil {
		return nil, err
	}
	return s, nil
}

// Record appends an event and returns it with generated fields filled.
func (s *Store) Record(e Event) Event {
	if s == nil {
		return e
	}
	if e.When.IsZero() {
		e.When = time.Now().UTC()
	} else {
		e.When = e.When.UTC()
	}
	if e.Source == "" {
		e.Source = "portal"
	}
	if e.ID == "" {
		e.ID = e.When.Format("20060102T150405.000000000Z")
	}
	e.UserAgent = truncate(e.UserAgent, maxUALen)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ring[s.idx] = e
	s.idx = (s.idx + 1) % s.size
	if s.n < s.size {
		s.n++
	}
	_ = s.appendLocked(e)
	return e
}

// Recent returns the newest events first, optionally filtered by a case
// insensitive substring across action, actor, target, detail, IP and UA.
func (s *Store) Recent(limit int, query string) []Event {
	if s == nil {
		return nil
	}
	if limit <= 0 {
		limit = 100
	}
	query = strings.ToLower(strings.TrimSpace(query))

	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Event, 0, min(limit, s.n))
	for i := 0; i < s.n && len(out) < limit; i++ {
		pos := (s.idx - 1 - i + s.size) % s.size
		ev := s.ring[pos]
		if query != "" && !matchEvent(ev, query) {
			continue
		}
		out = append(out, ev)
	}
	return out
}

func matchEvent(e Event, query string) bool {
	hay := strings.ToLower(strings.Join([]string{
		e.Source, e.Actor, e.Action, e.Target, e.Detail, e.IP, e.UserAgent, e.RequestID, e.ID,
	}, " "))
	return strings.Contains(hay, query)
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (s *Store) appendLocked(e Event) error {
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640) // #nosec G304 -- path is under the portal state directory
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(e)
}

func (s *Store) loadTail() error {
	f, err := os.Open(s.path) // #nosec G304 -- path is under the portal state directory
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLine)
	var lines []Event
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		lines = append(lines, e)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(lines) > s.size {
		lines = lines[len(lines)-s.size:]
	}
	for _, e := range lines {
		s.ring[s.idx] = e
		s.idx = (s.idx + 1) % s.size
		if s.n < s.size {
			s.n++
		}
	}
	return nil
}
