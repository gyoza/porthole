package parse

import "testing"

func TestStripLeadingTime(t *testing.T) {
	cases := []struct{ in, want string }{
		{"15:04:05.000  GET  /", "GET  /"},
		{"18:27:28.000  WARN  cache miss", "WARN  cache miss"},
		{"2026-08-14T18:27:28Z WARN cache miss", "WARN cache miss"},
		{"2026-08-14 18:27:28 INFO worker", "INFO worker"},
		{"Fri, 14 Aug 2026 18:26:46 GMT | [GET] http://10.0.0.1/", "[GET] http://10.0.0.1/"},
		{"I0814 18:26:46.000001       1 main.go:40] start", "1 main.go:40] start"},
		{`{"start_time":"2026-08-14T18:27:28Z","msg":"ok"}`, `{"start_time":"2026-08-14T18:27:28Z","msg":"ok"}`},
		{"GET /ok 200", "GET /ok 200"},
	}
	for _, tc := range cases {
		if got := StripLeadingTime(tc.in); got != tc.want {
			t.Errorf("StripLeadingTime(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
