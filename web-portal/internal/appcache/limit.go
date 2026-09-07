package appcache

import (
	"strings"
	"sync"
	"time"
)

// DownloadLimiter caps APK downloads per client IP.
type DownloadLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*dlCounter
}

type dlCounter struct {
	n     int
	reset time.Time
}

// NewDownloadLimiter creates a per IP download gate.
func NewDownloadLimiter(limitPerHour int) *DownloadLimiter {
	if limitPerHour < 1 {
		limitPerHour = defaultDownloadLimit
	}
	return &DownloadLimiter{
		limit:  limitPerHour,
		window: time.Hour,
		hits:   make(map[string]*dlCounter),
	}
}

// SetLimit updates the hourly cap.
func (l *DownloadLimiter) SetLimit(limitPerHour int) {
	if limitPerHour < 1 {
		limitPerHour = defaultDownloadLimit
	}
	l.mu.Lock()
	l.limit = limitPerHour
	l.mu.Unlock()
}

// Allow reports whether ip may download now.
func (l *DownloadLimiter) Allow(ip string) (bool, time.Duration) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gc(now)
	c := l.hits[ip]
	if c == nil || now.After(c.reset) {
		l.hits[ip] = &dlCounter{n: 1, reset: now.Add(l.window)}
		return true, 0
	}
	if c.n >= l.limit {
		return false, c.reset.Sub(now)
	}
	c.n++
	return true, 0
}

func (l *DownloadLimiter) gc(now time.Time) {
	for ip, c := range l.hits {
		if now.After(c.reset) {
			delete(l.hits, ip)
		}
	}
}
