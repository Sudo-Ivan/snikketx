package health

import (
	"sync"
	"time"
)

type Component struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Detail  string `json:"detail"`
	Hint    string `json:"hint,omitempty"`
	Latency string `json:"latency,omitempty"`
}

type ErrorEntry struct {
	ID        string    `json:"id"`
	Message   string    `json:"message"`
	When      time.Time `json:"when"`
	RequestID string    `json:"request_id,omitempty"`
}

type Ring struct {
	mu   sync.Mutex
	buf  []ErrorEntry
	size int
	idx  int
	n    int
}

func NewRing(size int) *Ring {
	if size < 1 {
		size = 32
	}
	return &Ring{buf: make([]ErrorEntry, size), size: size}
}

func (r *Ring) Add(e ErrorEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.When.IsZero() {
		e.When = time.Now().UTC()
	}
	r.buf[r.idx] = e
	r.idx = (r.idx + 1) % r.size
	if r.n < r.size {
		r.n++
	}
}

func (r *Ring) Recent() []ErrorEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ErrorEntry, 0, r.n)
	for i := 0; i < r.n; i++ {
		pos := (r.idx - 1 - i + r.size) % r.size
		out = append(out, r.buf[pos])
	}
	return out
}
