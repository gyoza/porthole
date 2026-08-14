// Package parse turns a raw log line into a structured record.
//
// It automatically detects JSON (including lines with a text prefix, as some
// Envoy Gateway / collector setups emit) and extracts common application and
// HTTP access-log fields so the TUI can render a compact one-liner while
// keeping the original JSON for the detail pane.
package parse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Kind classifies how a line should be rendered.
type Kind int

const (
	KindPlain Kind = iota
	KindApp
	KindHTTP
)

// Record is a parsed log line.
type Record struct {
	Raw       string
	JSONBytes []byte
	Fields    map[string]any
	Kind      Kind

	Timestamp time.Time
	Level     string
	Message   string

	Method    string
	Path      string
	Status    int
	Duration  string
	Host      string
	RequestID string
	Protocol  string
	Flags     string

	// Display is the compact one-line rendering (no ANSI).
	Display string
	// Flat is a searchable concatenation of field values.
	Flat string
}

var (
	timeKeys = []string{
		"start_time", "time", "ts", "timestamp", "@timestamp",
		"datetime", "date", "logged_at",
	}
	levelKeys  = []string{"level", "severity", "lvl", "log.level", "loglevel"}
	msgKeys    = []string{"msg", "message", "error", "err", "log"}
	methodKeys = []string{"method", "http.method", "req.method", "requestMethod"}
	pathKeys   = []string{
		"x-envoy-origin-path", "path", "url", "uri",
		"http.path", "http.url", "request.path", "request_path",
	}
	statusKeys = []string{
		"response_code", "status", "statusCode", "status_code",
		"http.status_code", "http_status",
	}
	durationKeys = []string{
		"duration", "dur", "elapsed", "latency", "response_time",
		"x-envoy-upstream-service-time", "took",
	}
	hostKeys = []string{
		":authority", "authority", "host", "http.host",
		"requested_server_name", "server_name",
	}
	reqIDKeys = []string{
		"x-request-id", "request_id", "requestId", "req_id",
		"trace_id", "traceId", "traceid",
	}
	protoKeys = []string{"protocol", "proto", "http.protocol"}
	flagKeys  = []string{"response_flags", "response_code_details", "flags"}
)

// Line parses a single raw log line.
func Line(raw string) Record {
	rec := Record{Raw: raw, Kind: KindPlain}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		rec.Display = raw
		return rec
	}

	obj, js, ok := extractJSON(trimmed)
	if !ok {
		return parsePlain(raw, trimmed)
	}

	unwrapNested(obj)
	rec.Fields = obj
	rec.JSONBytes = js

	rec.Timestamp = firstTime(obj, timeKeys...)
	rec.Level = strings.ToUpper(firstString(obj, levelKeys...))
	rec.Message = firstString(obj, msgKeys...)
	rec.Method = strings.ToUpper(firstString(obj, methodKeys...))
	rec.Path = firstString(obj, pathKeys...)
	rec.Status = firstInt(obj, statusKeys...)
	rec.Duration = formatDuration(firstAny(obj, durationKeys...))
	rec.Host = firstString(obj, hostKeys...)
	rec.RequestID = firstString(obj, reqIDKeys...)
	rec.Protocol = firstString(obj, protoKeys...)
	rec.Flags = firstString(obj, flagKeys...)

	switch {
	case rec.Method != "" || rec.Status > 0 || looksEnvoy(obj):
		rec.Kind = KindHTTP
		if rec.Path == "" {
			rec.Path = firstString(obj, "path", "x-envoy-origin-path")
		}
	case rec.Level != "" || rec.Message != "":
		rec.Kind = KindApp
	default:
		rec.Kind = KindApp
	}

	rec.Display = formatDisplay(rec)
	rec.Flat = flatten(obj)
	return rec
}

func looksEnvoy(m map[string]any) bool {
	_, hasStart := lookup(m, "start_time")
	_, hasPath := lookup(m, "x-envoy-origin-path")
	_, hasCode := lookup(m, "response_code")
	_, hasUp := lookup(m, "upstream_cluster")
	return (hasStart && (hasPath || hasCode)) || hasUp
}

func extractJSON(s string) (map[string]any, []byte, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return nil, nil, false
	}
	// Walk every '{' in case a prefix contains a brace.
	for start >= 0 && start < len(s) {
		dec := json.NewDecoder(strings.NewReader(s[start:]))
		dec.UseNumber()
		var m map[string]any
		if err := dec.Decode(&m); err == nil && len(m) > 0 {
			end := start + int(dec.InputOffset())
			if end > len(s) {
				end = len(s)
			}
			return m, []byte(s[start:end]), true
		}
		next := strings.IndexByte(s[start+1:], '{')
		if next < 0 {
			break
		}
		start += 1 + next
	}
	return nil, nil, false
}

func unwrapNested(m map[string]any) {
	for _, key := range []string{"message", "msg", "log"} {
		v, ok := lookup(m, key)
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if !strings.HasPrefix(s, "{") {
			continue
		}
		inner, _, ok := extractJSON(s)
		if !ok {
			continue
		}
		for k, iv := range inner {
			if _, exists := m[k]; !exists {
				m[k] = iv
			}
		}
	}
}

func formatDisplay(r Record) string {
	ts := ""
	if !r.Timestamp.IsZero() {
		ts = r.Timestamp.Format("15:04:05.000")
	}

	switch r.Kind {
	case KindHTTP:
		parts := make([]string, 0, 8)
		if ts != "" {
			parts = append(parts, ts)
		}
		if r.Method != "" {
			parts = append(parts, fmt.Sprintf("%-6s", r.Method))
		}
		if r.Path != "" {
			parts = append(parts, r.Path)
		}
		if r.Status > 0 {
			parts = append(parts, strconv.Itoa(r.Status))
		}
		if r.Duration != "" {
			parts = append(parts, r.Duration)
		}
		if r.Host != "" {
			parts = append(parts, r.Host)
		}
		if r.Flags != "" && r.Flags != "-" {
			parts = append(parts, r.Flags)
		}
		if r.Protocol != "" {
			parts = append(parts, r.Protocol)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "  ")
		}
	case KindApp:
		parts := make([]string, 0, 6)
		if ts != "" {
			parts = append(parts, ts)
		}
		if r.Level != "" {
			parts = append(parts, fmt.Sprintf("%-5s", r.Level))
		}
		if r.Message != "" {
			parts = append(parts, r.Message)
		}
		extras := extraKV(r)
		if extras != "" {
			parts = append(parts, extras)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "  ")
		}
	}
	if len(r.JSONBytes) > 0 {
		return string(compactJSON(r.JSONBytes))
	}
	return r.Raw
}

func extraKV(r Record) string {
	if r.Fields == nil {
		return ""
	}
	skip := map[string]struct{}{
		"time": {}, "ts": {}, "timestamp": {}, "@timestamp": {}, "start_time": {},
		"level": {}, "severity": {}, "lvl": {}, "msg": {}, "message": {},
	}
	var b strings.Builder
	keys := make([]string, 0, len(r.Fields))
	for k := range r.Fields {
		lk := strings.ToLower(k)
		if _, ok := skip[lk]; ok {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 4 {
		keys = keys[:4]
	}
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(stringify(r.Fields[k]))
	}
	return b.String()
}

func flatten(m map[string]any) string {
	var b strings.Builder
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for _, iv := range t {
				walk(iv)
			}
		case []any:
			for _, iv := range t {
				walk(iv)
			}
		default:
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(stringify(t))
		}
	}
	walk(m)
	return b.String()
}

// SearchPieces are independent strings the live regex is applied to.
// JSON is not searched as one blob, so response_code.*500 cannot jump
// from the status field into a later 500 in duration, bytes, or an id.
func (r Record) SearchPieces() []string {
	out := []string{r.Display, r.Level, r.Message, r.Method, r.Path, r.Host, r.Duration, r.Flags, r.Protocol, r.RequestID}
	if r.Status > 0 {
		st := strconv.Itoa(r.Status)
		out = append(out,
			st,
			"response_code="+st,
			`"response_code":`+st,
			`"response_code": `+st,
			"status="+st,
		)
	}
	if r.Fields != nil {
		collectFields(r.Fields, "", &out)
		out = append(out, rawOutsideJSON(r)...)
	} else if r.Raw != "" {
		out = append(out, r.Raw)
	}
	return out
}

// rawOutsideJSON is the text before/after the extracted JSON object —
// CRI prefixes, a second object, or a UUID sitting next to the blob.
func rawOutsideJSON(r Record) []string {
	if r.Raw == "" {
		return nil
	}
	if len(r.JSONBytes) == 0 {
		return []string{r.Raw}
	}
	js := string(r.JSONBytes)
	i := strings.Index(r.Raw, js)
	if i < 0 {
		return []string{r.Raw}
	}
	var out []string
	if i > 0 {
		out = append(out, r.Raw[:i])
	}
	if end := i + len(js); end < len(r.Raw) {
		out = append(out, r.Raw[end:])
	}
	return out
}

func collectFields(m map[string]any, prefix string, out *[]string) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch t := v.(type) {
		case map[string]any:
			collectFields(t, path, out)
		case []any:
			for _, iv := range t {
				if nested, ok := iv.(map[string]any); ok {
					collectFields(nested, path, out)
				} else {
					val := stringify(iv)
					*out = append(*out, path, val, path+"="+val)
				}
			}
		default:
			val := stringify(t)
			*out = append(*out,
				path,
				val,
				path+"="+val,
				`"`+path+`":`+val,
				`"`+path+`": `+val,
			)
		}
	}
}

// Pretty returns indented JSON when the line contained an object.
func (r Record) Pretty() string {
	if len(r.JSONBytes) == 0 {
		return r.Raw
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, r.JSONBytes, "", "  "); err != nil {
		return string(r.JSONBytes)
	}
	return buf.String()
}

func compactJSON(b []byte) []byte {
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return b
	}
	return buf.Bytes()
}

func firstString(m map[string]any, keys ...string) string {
	v := firstAny(m, keys...)
	if v == nil {
		return ""
	}
	return stringify(v)
}

func firstInt(m map[string]any, keys ...string) int {
	v := firstAny(m, keys...)
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err == nil {
			return n
		}
	}
	return 0
}

func firstTime(m map[string]any, keys ...string) time.Time {
	v := firstAny(m, keys...)
	if v == nil {
		return time.Time{}
	}
	switch t := v.(type) {
	case string:
		return parseTime(t)
	case json.Number:
		// unix seconds or millis
		if f, err := t.Float64(); err == nil {
			return unixGuess(f)
		}
	case float64:
		return unixGuess(t)
	}
	return time.Time{}
}

func unixGuess(f float64) time.Time {
	if f > 1e12 {
		return time.UnixMilli(int64(f))
	}
	if f > 1e9 {
		return time.Unix(int64(f), 0)
	}
	return time.Time{}
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05",
		"2006/01/02 15:04:05",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func firstAny(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := lookup(m, k); ok && v != nil {
			return v
		}
	}
	return nil
}

func lookup(m map[string]any, key string) (any, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	lk := strings.ToLower(key)
	for k, v := range m {
		if strings.ToLower(k) == lk {
			return v, true
		}
	}
	return nil, false
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

func formatDuration(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" || s == "-" {
			return ""
		}
		// already has a unit?
		if hasDurationUnit(s) {
			return s
		}
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return formatMillis(n)
		}
		return s
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return formatMillis(f)
		}
		return t.String()
	case float64:
		return formatMillis(t)
	case int:
		return formatMillis(float64(t))
	}
	return stringify(v)
}

func hasDurationUnit(s string) bool {
	s = strings.ToLower(s)
	return strings.HasSuffix(s, "ms") || strings.HasSuffix(s, "us") ||
		strings.HasSuffix(s, "µs") || strings.HasSuffix(s, "ns") ||
		strings.HasSuffix(s, "s") || strings.HasSuffix(s, "m")
}

func formatMillis(n float64) string {
	if n >= 1000 {
		return fmt.Sprintf("%.2fs", n/1000)
	}
	if n == float64(int64(n)) {
		return fmt.Sprintf("%dms", int64(n))
	}
	return fmt.Sprintf("%.1fms", n)
}
