package authlimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultIPLimit      = 30
	defaultAccountLimit = 8
	defaultWindow       = time.Minute
	defaultLockout      = 15 * time.Minute
	defaultMinLatency   = 300 * time.Millisecond
	defaultMaxPassword  = 512
	defaultMaxLocalpart = 1023
)

type Limiter struct {
	mu sync.Mutex

	ipLimit      int
	accountLimit int
	window       time.Duration
	lockout      time.Duration
	minLatency   time.Duration

	ipHits      map[string]*counter
	accountHits map[string]*counter
	locks       map[string]time.Time
}

type counter struct {
	n     int
	reset time.Time
}

type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

func New() *Limiter {
	return &Limiter{
		ipLimit:      defaultIPLimit,
		accountLimit: defaultAccountLimit,
		window:       defaultWindow,
		lockout:      defaultLockout,
		minLatency:   defaultMinLatency,
		ipHits:       make(map[string]*counter),
		accountHits:  make(map[string]*counter),
		locks:        make(map[string]time.Time),
	}
}

// NewFast is for tests. It keeps the lockout counters but skips the artificial
// login delay so suites stay quick.
func NewFast() *Limiter {
	l := New()
	l.minLatency = 0
	l.ipLimit = 10_000
	l.accountLimit = 10_000
	return l
}

func (l *Limiter) MinLatency() time.Duration { return l.minLatency }
func (l *Limiter) MaxPasswordLen() int       { return defaultMaxPassword }
func (l *Limiter) MaxLocalpartLen() int      { return defaultMaxLocalpart }

func (l *Limiter) Allow(ip, localpart string) Decision {
	now := time.Now()
	key := strings.ToLower(localpart) + "|" + ip

	l.mu.Lock()
	defer l.mu.Unlock()
	l.gc(now)

	if until, ok := l.locks[key]; ok && until.After(now) {
		return Decision{Allowed: false, RetryAfter: until.Sub(now)}
	}

	if !l.bump(l.ipHits, ip, l.ipLimit, now) {
		l.locks[key] = now.Add(l.lockout)
		return Decision{Allowed: false, RetryAfter: l.lockout}
	}
	if localpart != "" && !l.bump(l.accountHits, key, l.accountLimit, now) {
		l.locks[key] = now.Add(l.lockout)
		return Decision{Allowed: false, RetryAfter: l.lockout}
	}
	return Decision{Allowed: true}
}

func (l *Limiter) Failure(ip, localpart string) {
	now := time.Now()
	key := strings.ToLower(localpart) + "|" + ip
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.accountHits[key]
	if c == nil {
		c = &counter{reset: now.Add(l.window)}
		l.accountHits[key] = c
	}
	c.n++
	if c.n >= l.accountLimit {
		l.locks[key] = now.Add(l.lockout)
	}
}

func (l *Limiter) Success(ip, localpart string) {
	key := strings.ToLower(localpart) + "|" + ip
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.locks, key)
	delete(l.accountHits, key)
}

// LockEntry is one portal login lockout visible to administrators.
type LockEntry struct {
	Key        string
	Localpart  string
	IP         string
	Until      time.Time
	RetryAfter time.Duration
}

// Locks returns the active portal login lockouts.
func (l *Limiter) Locks() []LockEntry {
	if l == nil {
		return nil
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gc(now)
	out := make([]LockEntry, 0, len(l.locks))
	for key, until := range l.locks {
		if !until.After(now) {
			continue
		}
		localpart, ip, _ := strings.Cut(key, "|")
		out = append(out, LockEntry{
			Key:        key,
			Localpart:  localpart,
			IP:         ip,
			Until:      until,
			RetryAfter: until.Sub(now).Round(time.Second),
		})
	}
	return out
}

// Unlock clears a portal login lockout by its composite key.
func (l *Limiter) Unlock(key string) bool {
	if l == nil {
		return false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.locks[key]; !ok {
		return false
	}
	delete(l.locks, key)
	delete(l.accountHits, key)
	return true
}

func (l *Limiter) bump(m map[string]*counter, key string, limit int, now time.Time) bool {
	c := m[key]
	if c == nil || now.After(c.reset) {
		m[key] = &counter{n: 1, reset: now.Add(l.window)}
		return true
	}
	c.n++
	return c.n <= limit
}

func (l *Limiter) gc(now time.Time) {
	if len(l.ipHits)+len(l.accountHits)+len(l.locks) < 4096 {
		return
	}
	for k, c := range l.ipHits {
		if now.After(c.reset) {
			delete(l.ipHits, k)
		}
	}
	for k, c := range l.accountHits {
		if now.After(c.reset) {
			delete(l.accountHits, k)
		}
	}
	for k, until := range l.locks {
		if !until.After(now) {
			delete(l.locks, k)
		}
	}
}

func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
