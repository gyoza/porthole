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

func TestLastCRSegmentNoSplitBomb(t *testing.T) {
	if got := lastCRSegment("a\rb\r"); got != "b" {
		t.Fatalf("got %q", got)
	}
	if got := lastCRSegment("\r\r\r"); got != "" {
		t.Fatalf("all cr: %q", got)
	}
	if got := lastCRSegment("only"); got != "only" {
		t.Fatalf("no cr: %q", got)
	}
}

func TestSanitizeKeepsSGRColors(t *testing.T) {
	in := "\x1b[32mGET\x1b[0m  \x1b[38;2;232;238;244m/ok\x1b[0m"
	got := Sanitize(in)
	if got != in {
		t.Fatalf("SGR colors must stay:\n got %q\nwant %q", got, in)
	}
	mixed := "\x1b[32mGET\x1b[0m\x1b[K\x1b[1G /ok"
	got = Sanitize(mixed)
	if !strings.Contains(got, "\x1b[32mGET\x1b[0m") {
		t.Fatalf("kept color missing: %q", got)
	}
	if strings.Contains(got, "[K") || strings.Contains(got, "[1G") {
		t.Fatalf("cursor/erase should be gone: %q", got)
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
