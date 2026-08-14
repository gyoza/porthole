package parse

import (
	"strings"
	"unicode/utf8"
)

// Sanitize makes a log line safe to draw in a TUI row.
//
// curl, awscli, and similar tools rewrite a progress bar with '\r'.
// If that byte reaches the terminal the cursor jumps to column 0 and
// paints over [sources]. Tabs also break lipgloss width vs the tty.
func Sanitize(s string) string {
	if s == "" || !needsSanitize(s) {
		return s
	}
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		parts := strings.Split(s, "\r")
		s = ""
		for j := len(parts) - 1; j >= 0; j-- {
			if parts[j] != "" {
				s = parts[j]
				break
			}
		}
	}
	if strings.IndexByte(s, '\t') >= 0 {
		s = strings.ReplaceAll(s, "\t", "    ")
	}
	if strings.IndexByte(s, '\n') >= 0 {
		s = strings.ReplaceAll(s, "\n", " ")
	}
	if needsSanitize(s) {
		s = stripControls(s)
	}
	return s
}

func needsSanitize(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func stripControls(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c == 0x1b {
			i = skipEscape(s, i)
			continue
		}
		if c < 0x20 || c == 0x7f {
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		if size < 1 {
			size = 1
		}
		b.WriteString(s[i : i+size])
		i += size
	}
	return b.String()
}

func skipEscape(s string, i int) int {
	if i+1 >= len(s) {
		return i + 1
	}
	switch s[i+1] {
	case '[': // CSI
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j < len(s) {
			j++
		}
		return j
	case ']': // OSC
		j := i + 2
		for j < len(s) {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
			j++
		}
		return j
	default:
		return i + 2
	}
}
