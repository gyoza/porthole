package parse

import (
	"bufio"
	"os"
	"testing"
)

func TestMixedLogFile(t *testing.T) {
	f, err := os.Open("../../testdata/mixed.log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var kinds []Kind
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		kinds = append(kinds, Line(sc.Text()).Kind)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	want := []Kind{KindHTTP, KindApp, KindApp, KindApp, KindHTTP, KindHTTP, KindPlain, KindApp, KindHTTP}
	if len(kinds) != len(want) {
		t.Fatalf("got %d lines, want %d", len(kinds), len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("line %d: kind=%v want %v", i+1, kinds[i], want[i])
		}
	}
}
