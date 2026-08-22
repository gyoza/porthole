package parse

import (
	"strings"
	"unicode/utf8"
)

// Sanitize makes a log line safe to draw in a TUI row.
//
// curl, awscli, and similar tools rewrite a progress bar with '\r'.
// If that byte reaches the terminal the cursor jumps to column 0 and
// paints over [context]. Tabs also break lipgloss width vs the tty.
// SGR color/style sequences (…m) are kept so [logs] paint survives.
func Sanitize(s string) string {
	if s == "" || !needsSanitize(s) {
		return s
	}
	if strings.IndexByte(s, '\r') >= 0 {
		s = lastCRSegment(s)
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

// lastCRSegment keeps the final progress snapshot without Split-allocating
// every empty \r piece (a 1MiB of CRs would otherwise make ~1e6 strings).
func lastCRSegment(s string) string {
	for {
		i := strings.LastIndexByte(s, '\r')
		if i < 0 {
			return s
		}
		if i+1 < len(s) {
			return s[i+1:]
		}
		s = s[:i]
	}
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
			if j, keep := escapeSpan(s, i); keep {
				b.WriteString(s[i:j])
				i = j
				continue
			} else {
				i = j
				continue
			}
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

// escapeSpan returns the index after the escape at i.
// SGR color/style (CSI … m) is kept; cursor/erase/OSC is dropped.
func escapeSpan(s string, i int) (end int, keep bool) {
	if i+1 >= len(s) {
		return i + 1, false
	}
	switch s[i+1] {
	case '[': // CSI
		j := i + 2
		for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
			j++
		}
		if j < len(s) {
			keep = s[j] == 'm'
			j++
		}
		return j, keep
	case ']': // OSC
		j := i + 2
		for j < len(s) {
			if s[j] == 0x07 {
				return j + 1, false
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2, false
			}
			j++
		}
		return j, false
	default:
		return i + 2, false
	}
}
