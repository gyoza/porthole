package parse

import "testing"

func TestNginxCombined(t *testing.T) {
	r := Line(`10.1.0.4 - - [14/Aug/2026:21:00:01 +0000] "GET /index.html HTTP/1.1" 200 612`)
	if r.Kind != KindHTTP {
		t.Fatalf("kind=%v", r.Kind)
	}
	if r.Method != "GET" || r.Path != "/index.html" || r.Status != 200 {
		t.Fatalf("%s %s %d", r.Method, r.Path, r.Status)
	}
	if r.Timestamp.IsZero() {
		t.Fatal("missing ts")
	}
}

func TestStampLevel(t *testing.T) {
	r := Line(`2026-08-14T21:00:01Z INFO worker tick id=3`)
	if r.Kind != KindApp || r.Level != "INFO" {
		t.Fatalf("kind=%v level=%q", r.Kind, r.Level)
	}
	if r.Message != "worker tick id=3" {
		t.Fatalf("msg=%q", r.Message)
	}
}

func TestLevelFirst(t *testing.T) {
	r := Line(`[ERROR] failed to frobnicate widget`)
	if r.Kind != KindApp || r.Level != "ERROR" {
		t.Fatalf("kind=%v level=%q", r.Kind, r.Level)
	}
	if r.Message != "failed to frobnicate widget" {
		t.Fatalf("msg=%q", r.Message)
	}
}

func TestLogfmt(t *testing.T) {
	r := Line(`ts=2026-08-14T21:00:01Z level=error msg="upstream timeout" method=GET path=/api/orders status=504`)
	if r.Kind != KindHTTP {
		t.Fatalf("kind=%v", r.Kind)
	}
	if r.Level != "ERROR" || r.Method != "GET" || r.Path != "/api/orders" || r.Status != 504 {
		t.Fatalf("%+v", r)
	}
}

func TestKlog(t *testing.T) {
	r := Line(`E0814 21:00:01.123456       1 controller.go:88] reconciling widget`)
	if r.Kind != KindApp || r.Level != "ERROR" {
		t.Fatalf("kind=%v level=%q", r.Kind, r.Level)
	}
	if r.Message == "" {
		t.Fatal("empty message")
	}
}

func TestNginxError(t *testing.T) {
	r := Line(`2026/08/14 04:02:03 [error] 32#32: *3 open() "/usr/share/nginx/html/nope" failed (2: No such file or directory), client: 10.244.0.12, server: localhost, request: "GET /nope HTTP/1.1", host: "nginx.local"`)
	if r.Kind != KindApp || r.Level != "ERROR" {
		t.Fatalf("kind=%v level=%q", r.Kind, r.Level)
	}
	if r.Timestamp.IsZero() {
		t.Fatal("missing ts")
	}
}

func TestMixedStreamNoModeSwitch(t *testing.T) {
	// One function, no format flag: JSON, nginx, klog, and leftover text.
	lines := []struct {
		in   string
		kind Kind
	}{
		{`{"method":"GET","x-envoy-origin-path":"/","response_code":200}`, KindHTTP},
		{`10.1.0.4 - - [14/Aug/2026:21:00:01 +0000] "GET / HTTP/1.1" 200 612`, KindHTTP},
		{`2026-08-14T21:00:01Z INFO worker tick n=1`, KindApp},
		{`just a regular line`, KindPlain},
	}
	for _, tc := range lines {
		if g := Line(tc.in).Kind; g != tc.kind {
			t.Errorf("%q: kind=%v want %v", tc.in, g, tc.kind)
		}
	}
}

func TestStillPlainWhenNothingFits(t *testing.T) {
	r := Line("just a regular line")
	if r.Kind != KindPlain || r.Display != "just a regular line" {
		t.Fatalf("%v %q", r.Kind, r.Display)
	}
}
