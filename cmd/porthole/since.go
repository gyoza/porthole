package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// sinceValue is a pflag.Value so -s5m and -s1d work like Stern.
// Go's time.ParseDuration has no "d"/"w"; Stern users type those anyway.
type sinceValue struct{ d *time.Duration }

func newSinceValue(d *time.Duration) *sinceValue { return &sinceValue{d: d} }

func (s *sinceValue) String() string {
	if s == nil || s.d == nil {
		return "0"
	}
	return s.d.String()
}

func (s *sinceValue) Set(v string) error {
	d, err := parseSince(v)
	if err != nil {
		return err
	}
	*s.d = d
	return nil
}

func (s *sinceValue) Type() string { return "duration" }

// sinceUnit matches one Go-style duration token, plus d (day) and w (week).
// Longer units first so "ms" is not parsed as "m"+"s".
var sinceUnit = regexp.MustCompile(`(?i)^([0-9]*\.?[0-9]+)(ns|us|µs|μs|ms|s|m|h|d|w)`)

func parseSince(s string) (time.Duration, error) {
	orig := strings.TrimSpace(s)
	s = orig
	if s == "" {
		return 0, fmt.Errorf("invalid duration %q", orig)
	}
	if s[0] == '+' {
		s = s[1:]
	}
	if s == "0" {
		return 0, nil
	}
	if s[0] == '-' {
		return 0, fmt.Errorf("since must be positive")
	}

	// Prefer the stdlib parser for the common 5m / 1h30m cases.
	if d, err := time.ParseDuration(s); err == nil {
		if d < 0 {
			return 0, fmt.Errorf("since must be positive")
		}
		return d, nil
	}

	var total time.Duration
	rest := s
	for rest != "" {
		m := sinceUnit.FindStringSubmatch(rest)
		if m == nil {
			return 0, fmt.Errorf("invalid duration %q", orig)
		}
		n, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", orig)
		}
		u, ok := sinceUnitDur(m[2])
		if !ok {
			return 0, fmt.Errorf("invalid duration %q", orig)
		}
		total += time.Duration(n * float64(u))
		rest = rest[len(m[0]):]
	}
	return total, nil
}

func sinceUnitDur(u string) (time.Duration, bool) {
	switch strings.ToLower(u) {
	case "ns":
		return time.Nanosecond, true
	case "us", "µs", "μs":
		return time.Microsecond, true
	case "ms":
		return time.Millisecond, true
	case "s":
		return time.Second, true
	case "m":
		return time.Minute, true
	case "h":
		return time.Hour, true
	case "d":
		return 24 * time.Hour, true
	case "w":
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}
