package parse

import (
	"strings"
	"testing"
	"time"
)

const envoyLine = `{"start_time":"2026-08-13T21:00:01.123456789Z","method":"GET","x-envoy-origin-path":"/get","protocol":"HTTP/1.1","response_code":200,"response_flags":"-","response_code_details":"via_upstream","bytes_received":0,"bytes_sent":1234,"duration":12,"x-envoy-upstream-service-time":"8","user-agent":"curl/8.5.0","x-request-id":"abc-123",":authority":"www.example.com","upstream_host":"10.1.0.5:8080","upstream_cluster":"httproute/default/backend/rule/0","requested_server_name":"www.example.com"}`

func TestEnvoyAccessLog(t *testing.T) {
	r := Line(envoyLine)
	if r.Kind != KindHTTP {
		t.Fatalf("kind=%v want HTTP", r.Kind)
	}
	if r.Method != "GET" {
		t.Errorf("method=%q", r.Method)
	}
	if r.Path != "/get" {
		t.Errorf("path=%q", r.Path)
	}
	if r.Status != 200 {
		t.Errorf("status=%d", r.Status)
	}
	if r.Host != "www.example.com" {
		t.Errorf("host=%q", r.Host)
	}
	if r.Duration != "12ms" {
		t.Errorf("duration=%q", r.Duration)
	}
	if r.Timestamp.IsZero() {
		t.Fatal("missing timestamp")
	}
	if !strings.Contains(r.Display, "GET") || !strings.Contains(r.Display, "/get") {
		t.Errorf("display=%q", r.Display)
	}
	if !strings.Contains(r.Pretty(), `"method": "GET"`) {
		t.Errorf("pretty=%s", r.Pretty())
	}
}

func TestPrefixedJSON(t *testing.T) {
	r := Line(`[2026-08-13T21:00:01Z] {"level":"info","msg":"hello","ts":"2026-08-13T21:00:01Z"}`)
	if r.Kind != KindApp {
		t.Fatalf("kind=%v", r.Kind)
	}
	if r.Level != "INFO" {
		t.Errorf("level=%q", r.Level)
	}
	if r.Message != "hello" {
		t.Errorf("msg=%q", r.Message)
	}
}

func TestNestedMessageJSON(t *testing.T) {
	r := Line(`{"message":"{\"method\":\"POST\",\"path\":\"/login\",\"status\":401}","ts":"2026-08-13T21:00:01Z"}`)
	if r.Method != "POST" {
		t.Errorf("method=%q", r.Method)
	}
	if r.Path != "/login" {
		t.Errorf("path=%q", r.Path)
	}
	if r.Status != 401 {
		t.Errorf("status=%d", r.Status)
	}
}

func TestZapControllerLog(t *testing.T) {
	r := Line(`{"level":"error","ts":"2026-08-13T21:00:01.000Z","logger":"gatewayapi","msg":"httproute reference grant missing","httproute":"default/api"}`)
	if r.Kind != KindApp {
		t.Fatalf("kind=%v", r.Kind)
	}
	if r.Level != "ERROR" {
		t.Errorf("level=%q", r.Level)
	}
	if r.Message != "httproute reference grant missing" {
		t.Errorf("msg=%q", r.Message)
	}
	if !r.Timestamp.Equal(time.Date(2026, 8, 13, 21, 0, 1, 0, time.UTC)) {
		t.Errorf("ts=%v", r.Timestamp)
	}
}

func TestPlainText(t *testing.T) {
	r := Line("just a regular line")
	if r.Kind != KindPlain {
		t.Fatalf("kind=%v", r.Kind)
	}
	if r.Display != "just a regular line" {
		t.Errorf("display=%q", r.Display)
	}
	if r.JSONBytes != nil {
		t.Fatal("expected no json")
	}
}

func TestPrettyPreservesKeys(t *testing.T) {
	r := Line(envoyLine)
	p := r.Pretty()
	if !strings.Contains(p, "start_time") || !strings.Contains(p, "upstream_cluster") {
		t.Fatalf("pretty missing fields:\n%s", p)
	}
}
