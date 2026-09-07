package health

import (
	"context"
	"testing"
	"time"

	"github.com/sudo-ivan/snikketx/web-portal/internal/hostmetrics"
)

func TestProbeDomainEmpty(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c := ProbeDomain(ctx, "")
	if c.OK {
		t.Fatal("expected empty domain to be degraded")
	}
}

func TestProbeLocalDevSkipped(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	tlsComp := ProbeTLS(ctx, "chat.localhost")
	httpsComp := ProbeHTTPS(ctx, "chat.localhost")
	s2s := ProbeS2S(ctx, "chat.localhost", "http://snikket_server:5280")
	push := ProbePush(ctx, "chat.localhost")
	turn := ProbeTURN(ctx, "chat.localhost", "http://snikket_server:5280")
	xmpp := ProbeXMPPTLS(ctx, "chat.localhost", "http://snikket_server:5280")
	if !tlsComp.OK || !httpsComp.OK || !s2s.OK || !push.OK || !turn.OK || !xmpp.OK {
		t.Fatalf("localhost probes should be skipped as OK: tls=%+v https=%+v s2s=%+v push=%+v turn=%+v xmpp=%+v",
			tlsComp, httpsComp, s2s, push, turn, xmpp)
	}
}

func TestFormatComponentDetail(t *testing.T) {
	t.Parallel()
	ok := Component{OK: true, Detail: "up", Hint: "ignored when ok"}
	if FormatComponentDetail(ok) != "up" {
		t.Fatalf("ok detail: %q", FormatComponentDetail(ok))
	}
	bad := Component{OK: false, Detail: "down", Hint: "Open TCP 5269"}
	if FormatComponentDetail(bad) != "down. Open TCP 5269" {
		t.Fatalf("bad detail: %q", FormatComponentDetail(bad))
	}
}

func TestServiceHost(t *testing.T) {
	t.Parallel()
	if got := serviceHost("http://snikket_server:5280/"); got != "snikket_server" {
		t.Fatalf("got %q", got)
	}
}

func TestProbeHostResources(t *testing.T) {
	t.Parallel()
	stats := hostmetrics.Collect()
	mem := ProbeMemory(stats)
	cpu := ProbeCPU(stats)
	pressure := ProbePressure(stats)
	if mem.Name != "memory" || cpu.Name != "CPU load" || pressure.Name != "resource pressure" {
		t.Fatalf("unexpected probe names: %q %q %q", mem.Name, cpu.Name, pressure.Name)
	}
}
