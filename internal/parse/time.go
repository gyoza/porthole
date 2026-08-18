package parse

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	reClock     = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}(?:\.\d+)?`)
	reISO       = regexp.MustCompile(`(?i)^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?`)
	reSlashDate = regexp.MustCompile(`^\d{4}/\d{2}/\d{2}[ T]\d{2}:\d{2}:\d{2}(?:\.\d+)?`)
	reRFC1123   = regexp.MustCompile(`(?i)^[A-Z]{3},?\s+\d{1,2}\s+[A-Z]{3}\s+\d{4}\s+\d{2}:\d{2}:\d{2}(?:\s+[A-Z]{2,5})?`)
	reNginxDate = regexp.MustCompile(`^\[\d{2}/[A-Za-z]{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4}\]`)
	reKlogHead  = regexp.MustCompile(`^[IWEF]\d{4}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?`)
)

// StripLeadingTime drops a leading clock so [logs] can hide timestamps.
// JSON objects are left alone (the stamp lives in a field, shown in [json]).
func StripLeadingTime(s string) string {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	if s == "" || s[0] == '{' {
		return s
	}
	for _, re := range []*regexp.Regexp{reISO, reSlashDate, reRFC1123, reNginxDate, reKlogHead, reClock} {
		if loc := re.FindStringIndex(s); loc != nil && loc[0] == 0 {
			rest := strings.TrimLeftFunc(s[loc[1]:], unicode.IsSpace)
			rest = strings.TrimLeft(rest, "|-:")
			return strings.TrimLeftFunc(rest, unicode.IsSpace)
		}
	}
	return s
}
