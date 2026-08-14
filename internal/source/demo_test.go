package source

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDemoSeedsEveryPod(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	ch := make(chan Event, 8192)
	go func() {
		_ = DemoWith(ctx, DemoConfig{Pods: 40, Rate: 400, Quiet: 6, Progress: 2}, ch)
		close(ch)
	}()
	seen := map[string]struct{}{}
	n := 0
	progress := 0
	for ev := range ch {
		seen[ev.SourceID()] = struct{}{}
		n++
		if strings.Contains(ev.Line, "\r") {
			progress++
		}
	}
	if len(seen) < 40 {
		t.Fatalf("sources=%d want 40 (every pod seeded)", len(seen))
	}
	if n < 80 {
		t.Fatalf("lines=%d, expected a burst after the seed", n)
	}
	if progress == 0 {
		t.Fatal("expected at least one awscli-style progress line")
	}
}

func TestDemoCadence(t *testing.T) {
	d, n := demoCadence(12)
	if n != 1 || d > time.Second {
		t.Fatalf("slow cadence %s x%d", d, n)
	}
	d, n = demoCadence(2000)
	if d != 20*time.Millisecond || n != 40 {
		t.Fatalf("fast cadence %s x%d", d, n)
	}
}
