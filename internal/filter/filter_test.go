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

func TestResponseCodeStarDoesNotMatchOther500s(t *testing.T) {
	ok := parse.Line(`{"method":"GET","x-envoy-origin-path":"/boom","response_code":500,"duration":4,"bytes_sent":12}`)
	bad := parse.Line(`{"method":"GET","x-envoy-origin-path":"/ok","response_code":200,"duration":500,"bytes_sent":1500}`)
	pat := Compile(`response_code.*500`)
	if !pat.Match(ok, "") {
		t.Fatal("should match status 500")
	}
	if pat.Match(bad, "") {
		t.Fatal("must not match response_code 200 just because 500 appears later")
	}
	if !Compile("500").Match(ok, "") {
		t.Fatal("bare 500 should still match a 500 line")
	}
}

func TestUUIDInJSONField(t *testing.T) {
	const id = "6ef0ac35-0794-46fe-bec6-c6d89a420a29"
	rec := parse.Line(`{"level":"info","msg":"ok","trace_id":"` + id + `"}`)
	if !Compile(id).Match(rec, "") {
		t.Fatal("uuid in a JSON field should match")
	}
}

func TestUUIDOutsideJSONObject(t *testing.T) {
	const id = "6ef0ac35-0794-46fe-bec6-c6d89a420a29"
	prefix := parse.Line(id + ` {"level":"info","msg":"ok"}`)
	if !Compile(id).Match(prefix, "") {
		t.Fatalf("uuid before JSON should match; display=%q", prefix.Display)
	}
	suffix := parse.Line(`{"level":"info","msg":"ok"} request_id=` + id)
	if !Compile(id).Match(suffix, "") {
		t.Fatalf("uuid after JSON should match; display=%q", suffix.Display)
	}
}

func TestJoinIncludes(t *testing.T) {
	f, err := Join([]string{"foo", "6ef0ac35-0794-46fe-bec6-c6d89a420a29"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.Match(parse.Line(`request 6ef0ac35-0794-46fe-bec6-c6d89a420a29 done`), "") {
		t.Fatal("joined include should match uuid")
	}
	if f.Match(parse.Line(`nothing here`), "") {
		t.Fatal("should not match")
	}
	if _, err := Join([]string{"[invalid"}); err == nil {
		t.Fatal("expected invalid join to error")
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
