// Package source produces a stream of log events from Kubernetes, files,
// stdin, or a built-in demo generator.
package source

import (
	"bufio"
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"math/rand"
	"os"
	"strings"
	"time"
)

// Event is one log line from a named origin.
type Event struct {
	Time      time.Time
	Namespace string
	Pod       string
	Container string
	Line      string
}

// SourceID is the short "ns/pod/container" label used in the TUI.
func (e Event) SourceID() string {
	parts := make([]string, 0, 3)
	if e.Namespace != "" {
		parts = append(parts, e.Namespace)
	}
	if e.Pod != "" {
		parts = append(parts, e.Pod)
	}
	if e.Container != "" {
		parts = append(parts, e.Container)
	}
	if len(parts) == 0 {
		return "stdin"
	}
	return strings.Join(parts, "/")
}

// ColorSeed is a stable key for assigning a pod color.
func (e Event) ColorSeed() string {
	if e.Pod != "" {
		if e.Namespace != "" {
			return e.Namespace + "/" + e.Pod
		}
		return e.Pod
	}
	return e.SourceID()
}

// HashColorIndex maps a seed onto a palette of n colors.
func HashColorIndex(seed string, n int) int {
	if n <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return int(h.Sum32() % uint32(n))
}

// ReaderConfig tails a file or stdin.
type ReaderConfig struct {
	Name      string
	Namespace string
	Pod       string
	Container string
}

// ReadLines scans r and sends one Event per line until EOF or cancel.
func ReadLines(ctx context.Context, r io.Reader, cfg ReaderConfig, out chan<- Event) error {
	sc := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		ev := Event{
			Time:      time.Now(),
			Namespace: cfg.Namespace,
			Pod:       cfg.Pod,
			Container: cfg.Container,
			Line:      sc.Text(),
		}
		if ev.Pod == "" {
			ev.Pod = cfg.Name
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- ev:
		}
	}
	return sc.Err()
}

// OpenFile opens a log file for ReadLines.
func OpenFile(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, nil
}

// Demo emits realistic Envoy Gateway / app JSON logs forever.
func Demo(ctx context.Context, out chan<- Event) error {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	sources := []Event{
		{Namespace: "envoy-gateway-system", Pod: "envoy-eg-7f8c9d", Container: "envoy"},
		{Namespace: "envoy-gateway-system", Pod: "envoy-eg-2a1b4c", Container: "envoy"},
		{Namespace: "envoy-gateway-system", Pod: "envoy-gateway-0", Container: "envoy-gateway"},
	}
	paths := []string{
		"/get", "/login", "/api/v1/users", "/api/v1/orders",
		"/healthz", "/ready", "/.well-known/openid-configuration",
		"/oauth2/token", "/metrics", "/favicon.ico",
	}
	hosts := []string{"www.example.com", "api.internal", "auth.example.com"}
	methods := []weighted{
		{"GET", 70}, {"POST", 18}, {"PUT", 5}, {"DELETE", 4}, {"PATCH", 3},
	}
	statuses := []weightedInt{
		{200, 72}, {204, 4}, {301, 3}, {304, 6}, {400, 3}, {401, 3}, {404, 5}, {500, 2}, {502, 1}, {503, 1},
	}

	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()

	ctrlEvery := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-tick.C:
			// jitter the next interval a bit
			tick.Reset(time.Duration(50+rng.Intn(180)) * time.Millisecond)

			ctrlEvery++
			var ev Event
			if ctrlEvery%9 == 0 {
				ev = sources[2]
				ev.Time = now
				ev.Line = controllerLine(now, rng)
			} else {
				ev = sources[rng.Intn(2)]
				ev.Time = now
				ev.Line = accessLine(now, rng, methods, statuses, paths, hosts)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- ev:
			}
		}
	}
}

type weighted struct {
	v string
	w int
}

type weightedInt struct {
	v int
	w int
}

func pick(rng *rand.Rand, items []weighted) string {
	total := 0
	for _, it := range items {
		total += it.w
	}
	n := rng.Intn(total)
	for _, it := range items {
		if n < it.w {
			return it.v
		}
		n -= it.w
	}
	return items[0].v
}

func pickInt(rng *rand.Rand, items []weightedInt) int {
	total := 0
	for _, it := range items {
		total += it.w
	}
	n := rng.Intn(total)
	for _, it := range items {
		if n < it.w {
			return it.v
		}
		n -= it.w
	}
	return items[0].v
}

func accessLine(now time.Time, rng *rand.Rand, methods []weighted, statuses []weightedInt, paths, hosts []string) string {
	method := pick(rng, methods)
	status := pickInt(rng, statuses)
	path := paths[rng.Intn(len(paths))]
	host := hosts[rng.Intn(len(hosts))]
	dur := rng.Intn(80) + 1
	if status >= 500 {
		dur += rng.Intn(200)
	}
	details := "via_upstream"
	flags := "-"
	switch status {
	case 404:
		details = "route_not_found"
	case 401:
		details = "denied"
	case 502, 503:
		details = "upstream_reset"
		flags = "URX"
	case 301, 304:
		details = "via_upstream"
	}
	upHost := fmt.Sprintf("10.1.%d.%d:8080", rng.Intn(4)+1, rng.Intn(250)+1)
	reqID := fmt.Sprintf("%08x-%04x-%04x", rng.Uint32(), rng.Intn(0xffff), rng.Intn(0xffff))
	return fmt.Sprintf(
		`{"start_time":"%s","method":"%s","x-envoy-origin-path":"%s","protocol":"HTTP/1.1","response_code":%d,"response_flags":"%s","response_code_details":"%s","bytes_received":%d,"bytes_sent":%d,"duration":%d,"x-envoy-upstream-service-time":"%d","user-agent":"curl/8.5.0","x-request-id":"%s",":authority":"%s","upstream_host":"%s","upstream_cluster":"httproute/default/backend/rule/0","downstream_remote_address":"10.0.0.%d:%d","requested_server_name":"%s","route_name":"httproute/default/backend/rule/0"}`,
		now.UTC().Format(time.RFC3339Nano),
		method, path, status, flags, details,
		rng.Intn(400), rng.Intn(4000)+80, dur, dur,
		reqID, host, upHost, rng.Intn(250)+1, 40000+rng.Intn(20000), host,
	)
}

func controllerLine(now time.Time, rng *rand.Rand) string {
	msgs := []string{
		`{"level":"info","ts":"%s","logger":"infrastructure","msg":"reconciling gateway","gateway":"default/eg"}`,
		`{"level":"info","ts":"%s","logger":"status","msg":"updated gateway status","gateway":"default/eg","addresses":1}`,
		`{"level":"debug","ts":"%s","logger":"xds","msg":"pushed snapshot","node":"envoy-eg","resources":4}`,
		`{"level":"warn","ts":"%s","logger":"provider","msg":"endpoint not yet ready","service":"backend","namespace":"default"}`,
		`{"level":"error","ts":"%s","logger":"gatewayapi","msg":"httproute reference grant missing","httproute":"default/api","backendRef":"other/svc"}`,
	}
	return fmt.Sprintf(msgs[rng.Intn(len(msgs))], now.UTC().Format(time.RFC3339Nano))
}
