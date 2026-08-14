package parse

import (
	"strings"
	"testing"
)

func TestSanitizeProgressCR(t *testing.T) {
	in := "100  1024    0  1024    0     0   100k      0 --:--:-- --:--:-- --:--:--  100k" +
		"\r100 35589  0 35589    0     0   847k      0 --:--:-- --:--:-- --:--:--  847k"
	got := Sanitize(in)
	if strings.ContainsRune(got, '\r') {
		t.Fatalf("cr leaked: %q", got)
	}
	if !strings.Contains(got, "35589") {
		t.Fatalf("expected last progress snapshot, got %q", got)
	}
	if strings.Contains(got, "1024") {
		t.Fatalf("should drop overwritten progress, got %q", got)
	}
}

func TestSanitizeTabsAndCursor(t *testing.T) {
	in := "Dload\tUpload\tTotal\x1b[K\x1b[1G100 35589"
	got := Sanitize(in)
	if strings.ContainsAny(got, "\t\r\n") || strings.ContainsRune(got, 0x1b) {
		t.Fatalf("controls leaked: %q", got)
	}
	if !strings.Contains(got, "    ") {
		t.Fatalf("tabs should become spaces: %q", got)
	}
}

func TestLineKeepsJSONAfterCRPrefix(t *testing.T) {
	raw := "progress...\r{\"level\":\"info\",\"msg\":\"hello\"}"
	r := Line(raw)
	if r.Message != "hello" {
		t.Fatalf("message=%q kind=%v raw=%q", r.Message, r.Kind, r.Raw)
	}
	if strings.ContainsRune(r.Raw, '\r') {
		t.Fatal("raw still has cr")
	}
}
