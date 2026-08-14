package source

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func TestNextBackoffCaps(t *testing.T) {
	d := time.Second
	for i := 0; i < 8; i++ {
		d = nextBackoff(d)
	}
	if d != 15*time.Second {
		t.Fatalf("backoff=%s", d)
	}
}

func TestConsumeWatchReconnectsOnClose(t *testing.T) {
	fw := watch.NewFake()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var added int
	done := make(chan bool, 1)
	go func() {
		alive := consumeWatch(ctx, fw, func(*corev1.Pod) { added++ }, func(*corev1.Pod) {})
		done <- alive
	}()

	fw.Add(&corev1.Pod{})
	fw.Stop()

	select {
	case alive := <-done:
		if !alive {
			t.Fatal("closed watch should request reconnect, not cancel")
		}
		if added != 1 {
			t.Fatalf("added=%d", added)
		}
	case <-ctx.Done():
		t.Fatal("consumeWatch hung")
	}
}

func TestConsumeWatchStopsOnCancel(t *testing.T) {
	fw := watch.NewFake()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- consumeWatch(ctx, fw, func(*corev1.Pod) {}, func(*corev1.Pod) {})
	}()
	cancel()
	select {
	case alive := <-done:
		if alive {
			t.Fatal("cancel should not reconnect")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hung")
	}
}

func TestSplitSrc(t *testing.T) {
	got := splitSrc("ns/pod/c")
	if len(got) != 3 || got[0] != "ns" || got[1] != "pod" || got[2] != "c" {
		t.Fatalf("%v", got)
	}
}

func TestLiveTailHoldsPastHTTPTimeout(t *testing.T) {
	if os.Getenv("PORTHOLE_LIVE") == "" {
		t.Skip("set PORTHOLE_LIVE=1 to hit the cluster")
	}
	opts := KubeOptions{
		Namespace: "envoy-gateway-system",
		PodQuery:  regexp.MustCompile(".*"),
		TailLines: 20,
	}
	cs, ns, err := BuildClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	ch := make(chan Event, 256)
	errc := make(chan error, 1)
	go func() { errc <- TailPods(ctx, cs, ns, opts, ch) }()

	n, bad := 0, 0
	deadline := time.After(22 * time.Second)
	for {
		select {
		case ev := <-ch:
			n++
			if strings.Contains(ev.Line, "tail failed") || strings.Contains(ev.Line, "pod watch closed") {
				bad++
				t.Log(ev.Line)
			}
		case err := <-errc:
			if n == 0 {
				t.Fatalf("no log lines: %v", err)
			}
			if bad > 0 {
				t.Fatalf("watch died (%d errors) after %d lines: %v", bad, n, err)
			}
			t.Logf("ok: %d lines, TailPods ended with %v", n, err)
			return
		case <-deadline:
			t.Fatalf("TailPods did not return; saw %d lines, %d watch errors", n, bad)
		}
	}
}
