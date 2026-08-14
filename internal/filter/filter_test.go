package filter

import (
	"testing"

	"github.com/gyoza/porthole/internal/parse"
)

func TestEmptyMatchesAll(t *testing.T) {
	f := Compile("")
	if !f.Valid() || !f.Match(parse.Line("anything"), "src") {
		t.Fatal("empty filter should match")
	}
}

func TestLiveInvalidKeepsError(t *testing.T) {
	f := Compile(`5[0-9`)
	if f.Valid() {
		t.Fatal("expected invalid")
	}
	if f.Regexp != nil {
		t.Fatal("invalid compile should not set regexp")
	}
}

func TestMatchDisplayAndRaw(t *testing.T) {
	rec := parse.Line(`{"method":"GET","x-envoy-origin-path":"/oauth2/token","response_code":401,"start_time":"2026-08-13T21:00:01Z"}`)
	ok := Compile("401").Match(rec, "ns/pod/envoy")
	if !ok {
		t.Fatal("should match status in display/raw")
	}
	if !Compile("oauth2").Match(rec, "") {
		t.Fatal("should match path")
	}
	if Compile("502").Match(rec, "") {
		t.Fatal("should not match 502")
	}
	if !Compile("envoy").Match(rec, "ns/pod/envoy") {
		t.Fatal("should match source")
	}
}
