package main

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestParseSince(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"1h", time.Hour},
		{"1h30m", 90 * time.Minute},
		{"90s", 90 * time.Second},
		{"1d", 24 * time.Hour},
		{"2d", 48 * time.Hour},
		{"1d2h", 26 * time.Hour},
		{"1w", 7 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"0", 0},
		{"0s", 0},
	}
	for _, tc := range cases {
		got, err := parseSince(tc.in)
		if err != nil {
			t.Errorf("parseSince(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseSince(%q)=%s want %s", tc.in, got, tc.want)
		}
	}
	for _, bad := range []string{"", "nope", "-5m", "1x"} {
		if _, err := parseSince(bad); err == nil {
			t.Errorf("parseSince(%q) should fail", bad)
		}
	}
}

func TestParseContexts(t *testing.T) {
	got, err := parseContexts([]string{"prod1", "prod2"})
	if err != nil || len(got) != 2 || got[0] != "prod1" || got[1] != "prod2" {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if _, err := parseContexts([]string{"prod1", "prod1"}); err == nil {
		t.Fatal("expected duplicate error")
	}
	if _, err := parseContexts([]string{"a", "b", "c"}); err == nil {
		t.Fatal("expected more-than-two error")
	}
	got, err = parseContexts(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty=%v err=%v", got, err)
	}
}

func TestSinceCLI(t *testing.T) {
	cases := []struct {
		args []string
		want time.Duration
	}{
		{[]string{"--since", "5m"}, 5 * time.Minute},
		{[]string{"-s", "5m"}, 5 * time.Minute},
		{[]string{"-s5m"}, 5 * time.Minute},
		{[]string{"-s1d"}, 24 * time.Hour},
		{[]string{"-s", "1d"}, 24 * time.Hour},
		{[]string{"--since=1h30m"}, 90 * time.Minute},
		{[]string{"-s2w"}, 14 * 24 * time.Hour},
	}
	for _, tc := range cases {
		var d time.Duration
		cmd := &cobra.Command{Use: "t", Run: func(cmd *cobra.Command, args []string) {}}
		cmd.Flags().VarP(newSinceValue(&d), "since", "s", "")
		cmd.SetArgs(tc.args)
		if err := cmd.Execute(); err != nil {
			t.Errorf("%v: %v", tc.args, err)
			continue
		}
		if d != tc.want {
			t.Errorf("%v => %s want %s", tc.args, d, tc.want)
		}
	}
}
