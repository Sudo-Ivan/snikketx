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
	if !tlsComp.OK || !httpsComp.OK {
		t.Fatalf("localhost probes should be skipped as OK: tls=%+v https=%+v", tlsComp, httpsComp)
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
