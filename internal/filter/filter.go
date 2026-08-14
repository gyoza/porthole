// Package filter compiles a live regex and matches parsed log records.
//
// An invalid pattern is kept as an error and the last valid expression
// continues to apply, so typing `/5[0-9` does not blank the log view.
package filter

import (
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

// Match reports whether the record (or source name) should be shown.
//
// Structured JSON is matched field-by-field (and against the compact
// display line). That way response_code.*500 hits status 500 only, not a
// 200 line that later contains 500 in duration, bytes, or an id.
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
	return false
}

// Valid reports whether the current pattern compiled (or is empty).
func (f Filter) Valid() bool {
	return f.Err == nil
}
