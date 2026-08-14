package parse

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	reNginx      = regexp.MustCompile(`^(\S+) \S+ \S+ \[([^\]]+)\] "([A-Z]+) ([^"]*?)(?: (HTTP/[\d.]+))?" (\d{3}) (\S+)`)
	reNginxErr   = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) \[(\w+)\] (.*)$`)
	reStampLevel = regexp.MustCompile(`^(?i)(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?)\s+\[?(DEBUG|INFO|WARN|WARNING|ERROR|FATAL|TRACE|ERR)\]?\s+(.*)$`)
	reLevelFirst = regexp.MustCompile(`^(?i)\[?(DEBUG|INFO|WARN|WARNING|ERROR|FATAL|TRACE|ERR)\]?\s+[-:]?\s*(.*)$`)
	reKlog       = regexp.MustCompile(`^([IWEF])(\d{4})\s+(\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+\d+\s+(\S+\.go:\d+)\]\s+(.*)$`)
)

func parsePlain(raw, trimmed string) Record {
	rec := Record{Raw: raw, Kind: KindPlain, Display: raw, Flat: raw}

	if parseNginx(&rec, trimmed) {
		return rec
	}
	if parseNginxError(&rec, trimmed) {
		return rec
	}
	if parseKlog(&rec, trimmed) {
		return rec
	}
	if parseStampLevel(&rec, trimmed) {
		return rec
	}
	if parseLogfmt(&rec, trimmed) {
		return rec
	}
	if parseLevelFirst(&rec, trimmed) {
		return rec
	}
	return rec
}

func parseNginx(rec *Record, s string) bool {
	m := reNginx.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	rec.Kind = KindHTTP
	rec.Host = m[1]
	if t, err := time.Parse("02/Jan/2006:15:04:05 -0700", m[2]); err == nil {
		rec.Timestamp = t
	}
	rec.Method = m[3]
	rec.Path = m[4]
	rec.Protocol = m[5]
	rec.Status, _ = strconv.Atoi(m[6])
	rec.Display = formatDisplay(*rec)
	rec.Flat = s
	return true
}

func parseNginxError(rec *Record, s string) bool {
	m := reNginxErr.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	if t, err := time.Parse("2006/01/02 15:04:05", m[1]); err == nil {
		rec.Timestamp = t
	}
	rec.Level = normLevel(m[2])
	rec.Message = m[3]
	rec.Kind = KindApp
	rec.Display = formatDisplay(*rec)
	rec.Flat = s
	return true
}

func parseKlog(rec *Record, s string) bool {
	m := reKlog.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	switch m[1] {
	case "I":
		rec.Level = "INFO"
	case "W":
		rec.Level = "WARN"
	case "E":
		rec.Level = "ERROR"
	case "F":
		rec.Level = "FATAL"
	}
	// m[2] is MMDD
	if t, err := time.Parse("0102 15:04:05.999999999", m[2]+" "+m[3]); err == nil {
		rec.Timestamp = t.AddDate(time.Now().Year(), 0, 0)
	}
	rec.Message = m[5]
	if m[4] != "" {
		rec.Message = rec.Message + "  " + m[4]
	}
	rec.Kind = KindApp
	rec.Display = formatDisplay(*rec)
	rec.Flat = s
	return true
}

func parseStampLevel(rec *Record, s string) bool {
	m := reStampLevel.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	rec.Timestamp = parseTime(m[1])
	rec.Level = normLevel(m[2])
	rec.Message = m[3]
	rec.Kind = KindApp
	rec.Display = formatDisplay(*rec)
	rec.Flat = s
	return true
}

func parseLevelFirst(rec *Record, s string) bool {
	m := reLevelFirst.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	rec.Level = normLevel(m[1])
	rec.Message = m[2]
	rec.Kind = KindApp
	rec.Display = formatDisplay(*rec)
	rec.Flat = s
	return true
}

func parseLogfmt(rec *Record, s string) bool {
	if !strings.Contains(s, "=") {
		return false
	}
	fields := scanLogfmt(s)
	if len(fields) < 2 {
		return false
	}
	if _, ok := fields["level"]; !ok {
		if _, ok := fields["msg"]; !ok {
			if _, ok := fields["message"]; !ok {
				return false
			}
		}
	}
	rec.Fields = fields
	rec.Timestamp = firstTime(fields, timeKeys...)
	rec.Level = normLevel(firstString(fields, levelKeys...))
	rec.Message = firstString(fields, msgKeys...)
	rec.Method = strings.ToUpper(firstString(fields, methodKeys...))
	rec.Path = firstString(fields, pathKeys...)
	rec.Status = firstInt(fields, statusKeys...)
	rec.Duration = formatDuration(firstAny(fields, durationKeys...))
	rec.Host = firstString(fields, hostKeys...)
	switch {
	case rec.Method != "" || rec.Status > 0:
		rec.Kind = KindHTTP
	default:
		rec.Kind = KindApp
	}
	rec.Display = formatDisplay(*rec)
	rec.Flat = flatten(fields)
	return true
}

func scanLogfmt(s string) map[string]any {
	out := map[string]any{}
	i := 0
	for i < len(s) {
		for i < len(s) && unicode.IsSpace(rune(s[i])) {
			i++
		}
		if i >= len(s) {
			break
		}
		eq := strings.IndexByte(s[i:], '=')
		if eq <= 0 {
			break
		}
		key := s[i : i+eq]
		i = i + eq + 1
		if i >= len(s) {
			out[key] = ""
			break
		}
		if s[i] == '"' {
			j := i + 1
			for j < len(s) {
				if s[j] == '\\' && j+1 < len(s) {
					j += 2
					continue
				}
				if s[j] == '"' {
					break
				}
				j++
			}
			val := s[i+1 : min(j, len(s))]
			out[key] = val
			i = j + 1
			continue
		}
		j := i
		for j < len(s) && !unicode.IsSpace(rune(s[j])) {
			j++
		}
		out[key] = s[i:j]
		i = j
	}
	return out
}

func normLevel(s string) string {
	switch strings.ToUpper(s) {
	case "WARNING":
		return "WARN"
	case "ERR":
		return "ERROR"
	default:
		return strings.ToUpper(s)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
