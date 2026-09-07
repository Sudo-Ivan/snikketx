package authlimit_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/quick"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/authlimit"
)

func TestLockoutAfterFailures(t *testing.T) {
	l := authlimit.New()
	ip := "203.0.113.10"
	user := "alice"
	for range 8 {
		_ = l.Allow(ip, user)
		l.Failure(ip, user)
	}
	d := l.Allow(ip, user)
	if d.Allowed {
		t.Fatal("expected lockout")
	}
	if d.RetryAfter <= 0 {
		t.Fatal("expected retry after")
	}
}

func TestSuccessClearsLock(t *testing.T) {
	l := authlimit.New()
	ip := "203.0.113.11"
	user := "bob"
	for range 8 {
		_ = l.Allow(ip, user)
		l.Failure(ip, user)
	}
	if l.Allow(ip, user).Allowed {
		t.Fatal("expected lock")
	}
	l.Success(ip, user)
	if !l.Allow(ip, user).Allowed {
		t.Fatal("success should clear lock")
	}
}

func TestRaceLimiter(t *testing.T) {
	l := authlimit.NewFast()
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for j := range 100 {
				d := l.Allow("127.0.0.1", "user")
				if !d.Allowed {
					continue
				}
				if j%2 == 0 {
					l.Failure("127.0.0.1", "user")
				} else {
					l.Success("127.0.0.1", "user")
				}
			}
		})
	}
	wg.Wait()
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.9:12345"
	if got := authlimit.ClientIP(req); got != "198.51.100.9" {
		t.Fatalf("got %q", got)
	}
}

func TestPropertyAllowThenSuccess(t *testing.T) {
	l := authlimit.NewFast()
	f := func(local string) bool {
		if len(local) > 64 {
			return true
		}
		d := l.Allow("127.0.0.1", local)
		if !d.Allowed {
			return true
		}
		l.Success("127.0.0.1", local)
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}

func TestMinLatencyDefaults(t *testing.T) {
	l := authlimit.New()
	if l.MinLatency() < 200*time.Millisecond {
		t.Fatalf("min latency too low: %s", l.MinLatency())
	}
	if l.MaxPasswordLen() < 64 {
		t.Fatal("password max too small")
	}
}
