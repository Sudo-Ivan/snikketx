package linkpreview

import (
	"strings"
	"sync"
	"time"
)

// Limiter caps preview lookups per authenticated account. It mirrors the
// fixed window counters used by authlimit and appcache.
type Limiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*userCounter
}

type userCounter struct {
	n     int
	reset time.Time
}

// NewLimiter creates a per account gate allowing limit lookups per hour.
func NewLimiter(limitPerHour int) *Limiter {
	if limitPerHour < 1 {
		limitPerHour = DefaultUserHourlyLimit
	}
	return &Limiter{
		limit:  limitPerHour,
		window: time.Hour,
		hits:   make(map[string]*userCounter),
	}
}

// Allow reports whether key may fetch a preview now, and how long to wait
// when it may not.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		key = "unknown"
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gc(now)
	c := l.hits[key]
	if c == nil || now.After(c.reset) {
		l.hits[key] = &userCounter{n: 1, reset: now.Add(l.window)}
		return true, 0
	}
	if c.n >= l.limit {
		return false, c.reset.Sub(now)
	}
	c.n++
	return true, 0
}

func (l *Limiter) gc(now time.Time) {
	if len(l.hits) < 4096 {
		return
	}
	for key, c := range l.hits {
		if now.After(c.reset) {
			delete(l.hits, key)
		}
	}
}
