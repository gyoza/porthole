package source

import (
	"context"
	"fmt"
	"math/rand"
	"time"
)

// DemoConfig sizes the in-process load generator (--demo).
// Zero values mean: 5 pods, 12 lines/s, no dedicated quiet/progress pods.
type DemoConfig struct {
	Pods     int
	Rate     int
	Quiet    int
	Progress int
}

func (c DemoConfig) norm() DemoConfig {
	if c.Pods <= 0 {
		c.Pods = 5
	}
	if c.Rate <= 0 {
		c.Rate = 12
	}
	if c.Quiet < 0 {
		c.Quiet = 0
	}
	if c.Progress < 0 {
		c.Progress = 0
	}
	if c.Quiet+c.Progress >= c.Pods {
		c.Quiet = max(0, c.Pods/8)
		c.Progress = min(2, max(0, c.Pods-c.Quiet-1))
	}
	return c
}

// Demo emits a mixed 5-pod stream (JSON + plain) without a cluster.
func Demo(ctx context.Context, out chan<- Event) error {
	return DemoWith(ctx, DemoConfig{}, out)
}

// DemoWith emits a mixed stream sized for local UI load tests.
// Every pod is seeded once so [context] is fully populated, then Rate
// lines/sec are sent. Quiet pods stay almost silent so they fall out of
// the 20k-line ring while remaining in [context]. Progress pods emit
// curl/awscli \r rewrites.
func DemoWith(ctx context.Context, cfg DemoConfig, out chan<- Event) error {
	cfg = cfg.norm()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	pods := makeFleet(cfg)
	paths := demoPaths
	hosts := demoHosts

	now := time.Now()
	for i := range pods {
		ev := pods[i].emit(now, rng, paths, hosts)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- ev:
		}
	}

	tickEvery, perTick := demoCadence(cfg.Rate)
	tick := time.NewTicker(tickEvery)
	defer tick.Stop()

	noisy, quiet := splitFleet(pods)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-tick.C:
			for n := 0; n < perTick; n++ {
				p := pickFleet(rng, noisy, quiet)
				ev := p.emit(now, rng, paths, hosts)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case out <- ev:
				}
			}
		}
	}
}

type demoKind int

const (
	kindAccess demoKind = iota
	kindApp
	kindNginx
	kindChatter
	kindProgress
)

type demoPod struct {
	Event
	kind     demoKind
	quiet    bool
	progress int
}

func makeFleet(cfg DemoConfig) []demoPod {
	ns := []string{"prod", "staging", "logs", "kube-system", "envoy-gateway-system"}
	kinds := []struct {
		kind      demoKind
		name      string
		container string
	}{
		{kindAccess, "envoy", "envoy"},
		{kindApp, "gateway", "envoy-gateway"},
		{kindNginx, "nginx", "nginx"},
		{kindChatter, "chatter", "chatter"},
		{kindChatter, "cpg2sp", "app"},
		{kindChatter, "portal", "portal"},
		{kindApp, "script", "script"},
	}
	out := make([]demoPod, 0, cfg.Pods)
	for i := 0; i < cfg.Pods; i++ {
		k := kinds[i%len(kinds)]
		p := demoPod{
			Event: Event{
				Namespace: ns[i%len(ns)],
				Pod:       fmt.Sprintf("%s-%s", k.name, shortID(i)),
				Container: k.container,
			},
			kind: k.kind,
		}
		switch {
		case i < cfg.Progress:
			p.kind = kindProgress
			p.Event.Container = "awscli"
			p.Event.Pod = fmt.Sprintf("sl-openbao-backup-%s", shortID(i))
		case i < cfg.Progress+cfg.Quiet:
			p.quiet = true
		}
		out = append(out, p)
	}
	return out
}

func splitFleet(pods []demoPod) (noisy, quiet []*demoPod) {
	for i := range pods {
		if pods[i].quiet {
			quiet = append(quiet, &pods[i])
		} else {
			noisy = append(noisy, &pods[i])
		}
	}
	if len(noisy) == 0 {
		return quiet, nil
	}
	return noisy, quiet
}

func pickFleet(rng *rand.Rand, noisy, quiet []*demoPod) *demoPod {
	if len(quiet) > 0 && rng.Intn(40) == 0 {
		return quiet[rng.Intn(len(quiet))]
	}
	return noisy[rng.Intn(len(noisy))]
}

func demoCadence(rate int) (time.Duration, int) {
	if rate <= 50 {
		return time.Second / time.Duration(max(1, rate)), 1
	}
	return 20 * time.Millisecond, max(1, rate/50)
}

func (p *demoPod) emit(now time.Time, rng *rand.Rand, paths, hosts []string) Event {
	ev := p.Event
	ev.Time = now
	switch p.kind {
	case kindAccess:
		ev.Line = accessLine(now, rng, demoMethods, demoStatuses, paths, hosts)
	case kindApp:
		ev.Line = controllerLine(now, rng)
	case kindNginx:
		ev.Line = nginxLine(now, rng, paths)
	case kindProgress:
		p.progress += 1024 + rng.Intn(4096)
		if p.progress > 40000 {
			p.progress = 1024
		}
		ev.Line = progressLine(p.progress)
	default:
		ev.Line = chatterLine(now, rng)
	}
	return ev
}

func shortID(i int) string {
	return fmt.Sprintf("%05x", (0x9e3779b1*uint32(i+1))&0xfffff)
}

func progressLine(n int) string {
	// One Event containing \r rewrites — the TUI must keep the last snapshot
	// and must not paint over [context].
	return fmt.Sprintf(
		"  %% Total    %% Received %% Xferd  Average Speed   Time    Time     Time  Current\r"+
			"                                 Dload  Upload   Total   Spent    Left  Speed\r"+
			"100 %d  0 %d    0     0   847k      0 --:--:-- --:--:-- --:--:--  847k",
		n, n,
	)
}

var (
	demoPaths = []string{
		"/get", "/login", "/api/v1/users", "/api/v1/orders",
		"/healthz", "/ready", "/.well-known/openid-configuration",
		"/oauth2/token", "/metrics", "/favicon.ico",
	}
	demoHosts   = []string{"www.example.com", "api.internal", "auth.example.com"}
	demoMethods = []weighted{
		{"GET", 70}, {"POST", 18}, {"PUT", 5}, {"DELETE", 4}, {"PATCH", 3},
	}
	demoStatuses = []weightedInt{
		{200, 72}, {204, 4}, {301, 3}, {304, 6}, {400, 3}, {401, 3}, {404, 5}, {500, 2}, {502, 1}, {503, 1},
	}
)

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
	reqID := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", rng.Uint32(), rng.Intn(0xffff), rng.Intn(0xffff), rng.Intn(0xffff), rng.Uint64()&0xffffffffffff)
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

func nginxLine(now time.Time, rng *rand.Rand, paths []string) string {
	path := paths[rng.Intn(len(paths))]
	if rng.Intn(10) == 0 {
		return fmt.Sprintf(`%s [error] 32#32: *%d open() "/usr/share/nginx/html%s" failed (2: No such file or directory), client: 10.1.0.%d, server: localhost, request: "GET %s HTTP/1.1", host: "nginx.local"`,
			now.UTC().Format("2006/01/02 15:04:05"), rng.Intn(90)+1, path, rng.Intn(250)+1, path)
	}
	return fmt.Sprintf(`10.1.0.%d - - [%s] "GET %s HTTP/1.1" %d %d "-" "curl/8.5.0" "-"`,
		rng.Intn(250)+1, now.UTC().Format("02/Jan/2006:15:04:05 +0000"), path, 200, 200+rng.Intn(800))
}

func chatterLine(now time.Time, rng *rand.Rand) string {
	ts := now.UTC().Format(time.RFC3339)
	switch rng.Intn(6) {
	case 0:
		return fmt.Sprintf("%s INFO  worker tick n=%d", ts, rng.Intn(500))
	case 1:
		return fmt.Sprintf("%s WARN  cache miss key=user:%d", ts, rng.Intn(80))
	case 2:
		return fmt.Sprintf("[ERROR] failed to frobnicate widget id=%d", rng.Intn(80))
	case 3:
		return fmt.Sprintf("I0814 %s.000001       1 main.go:40] starting workers n=%d", now.UTC().Format("15:04:05"), rng.Intn(80))
	case 4:
		return fmt.Sprintf("%s INFO  request_id=%s done", ts, fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", rng.Uint32(), rng.Intn(0xffff), rng.Intn(0xffff), rng.Intn(0xffff), rng.Uint64()&0xffffffffffff))
	default:
		return fmt.Sprintf(`ts=%s level=error msg="upstream timeout" method=GET path=/api/orders status=504`, ts)
	}
}
