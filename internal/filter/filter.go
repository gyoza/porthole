// Package filter compiles a live regex and matches parsed log records.
//
// An invalid pattern is kept as an error and the last valid expression
// continues to apply, so typing `/5[0-9` does not blank the log view.
package filter

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gyoza/porthole/internal/parse"
)

// Filter is a compiled live regex plus its source pattern.
type Filter struct {
	Pattern string
	Regexp  *regexp.Regexp
	Err     error
}

// Compile builds a filter from the user's current input.
// An empty pattern matches everything.
func Compile(pattern string) Filter {
	pattern = strings.TrimSpace(pattern)
	f := Filter{Pattern: pattern}
	if pattern == "" {
		return f
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		f.Err = err
		return f
	}
	f.Regexp = re
	return f
}

// Join compiles one or more Stern-style -i/-e patterns as alternatives.
// An empty list matches everything. A bad pattern is an error (startup).
func Join(patterns []string) (Filter, error) {
	var parts []string
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := regexp.Compile(p); err != nil {
			return Filter{}, fmt.Errorf("regex %q: %w", p, err)
		}
		parts = append(parts, "(?:"+p+")")
	}
	if len(parts) == 0 {
		return Filter{}, nil
	}
	f := Compile(strings.Join(parts, "|"))
	if f.Err != nil {
		return Filter{}, f.Err
	}
	return f, nil
}

// Match reports whether the record (or source name) should be shown.
//
// Structured JSON is matched field-by-field (and against the compact
// display line). That way response_code.*500 hits status 500 only, not a
// 200 line that later contains 500 in duration, bytes, or an id.
//
// Literal patterns (no unescaped ., *, +) also run against the original
// line, like Stern -i, so a UUID in a prefix or an unparsed field still hits.
func (f Filter) Match(rec parse.Record, source string) bool {
	if f.Regexp == nil {
		return true
	}
	if source != "" && f.Regexp.MatchString(source) {
		return true
	}
	for _, p := range rec.SearchPieces() {
		if p != "" && f.Regexp.MatchString(p) {
			return true
		}
	}
	if rec.Raw != "" && !canCrossFields(f.Pattern) && f.Regexp.MatchString(rec.Raw) {
		return true
	}
	return false
}

// canCrossFields is true when the pattern can jump from one JSON field
// into a later one (response_code.*500 matching a 200 line with duration 500).
func canCrossFields(pat string) bool {
	esc := false
	for _, r := range pat {
		if esc {
			esc = false
			continue
		}
		if r == '\\' {
			esc = true
			continue
		}
		if r == '.' || r == '*' || r == '+' {
			return true
		}
	}
	return false
}

// Valid reports whether the current pattern compiled (or is empty).
func (f Filter) Valid() bool {
	return f.Err == nil
}
