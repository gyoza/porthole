package source

import "testing"

func TestCleanKlog(t *testing.T) {
	in := "I0814 22:20:56.241213  42104 request.go:700] Waited for 1.17s due to client-side throttling"
	got := cleanKlog(in)
	if got != "Waited for 1.17s due to client-side throttling" {
		t.Fatalf("%q", got)
	}
}
