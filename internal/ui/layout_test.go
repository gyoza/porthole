package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/gyoza/porthole/internal/parse"
	"github.com/gyoza/porthole/internal/source"
)

func testModel(w, h int, detail, sources bool) *model {
	m := New(Options{Namespace: "envoy-gateway-system"}).(*model)
	m.width = w
	m.height = h
	m.showDetail = detail
	m.showSources = sources
	for i := 0; i < 8; i++ {
		line := fmt.Sprintf(`{"start_time":"2026-08-14T03:47:10.547Z","method":"GET","x-envoy-origin-path":"/api/orders","response_code":200,":authority":"www.example.com","duration":2}`)
		ln := logLine{
			Ev:     source.Event{Namespace: "envoy-gateway-system", Pod: "envoy-echo-eg-4a38950b-549695dd97-ht27k", Container: "envoy", Line: line},
			Rec:    parse.Line(line),
			Source: "envoy-gateway-system/envoy-echo-eg-4a38950b-549695dd97-ht27k/envoy",
			Color:  colorFor("envoy-gateway-system/envoy-echo-eg"),
		}
		m.lines = append(m.lines, ln)
		m.filtered = append(m.filtered, i)
		m.bumpSource(ln)
	}
	m.cursor = len(m.filtered) - 1
	m.relayout()
	return m
}

func TestViewFitsTerminal(t *testing.T) {
	sizes := []struct{ w, h int }{
		{80, 24},
		{100, 30},
		{120, 40},
		{160, 48},
		{200, 50},
	}
	for _, sz := range sizes {
		for _, detail := range []bool{false, true} {
			for _, sources := range []bool{false, true} {
				if sz.w < 100 {
					sources = false
				}
				m := testModel(sz.w, sz.h, detail, sources)
				view := m.View()
				gotW, gotH := lipgloss.Width(view), lipgloss.Height(view)
				if gotH > sz.h {
					t.Errorf("%dx%d detail=%v sources=%v: height %d > %d\n%s",
						sz.w, sz.h, detail, sources, gotH, sz.h, view)
				}
				if gotW > sz.w {
					t.Errorf("%dx%d detail=%v sources=%v: width %d > %d",
						sz.w, sz.h, detail, sources, gotW, sz.w)
				}
			}
		}
	}
}

func TestToggleDetailKeepsFrame(t *testing.T) {
	m := testModel(140, 40, false, true)
	off := m.View()
	m.showDetail = true
	m.relayout()
	on := m.View()
	if lipgloss.Height(off) != lipgloss.Height(on) {
		t.Fatalf("height shifted: off=%d on=%d", lipgloss.Height(off), lipgloss.Height(on))
	}
	if lipgloss.Width(off) != lipgloss.Width(on) {
		t.Fatalf("width shifted: off=%d on=%d", lipgloss.Width(off), lipgloss.Width(on))
	}
	if lipgloss.Height(on) > 40 {
		t.Fatalf("overflow %d", lipgloss.Height(on))
	}
}

func TestDetailClearsWhenSwitchingJSONToPlain(t *testing.T) {
	m := testModel(140, 40, true, true)
	plain := `2026-08-14T21:00:01Z INFO worker tick n=3`
	ln := logLine{
		Ev:     source.Event{Namespace: "logs", Pod: "chatter-1", Container: "chatter", Line: plain},
		Rec:    parse.Line(plain),
		Source: "logs/chatter-1/chatter",
		Color:  colorFor("logs/chatter"),
	}
	m.lines = append(m.lines, ln)
	m.filtered = append(m.filtered, len(m.lines)-1)

	m.cursor = 0
	m.refreshDetail()
	jsonView := m.View()
	if !strings.Contains(jsonView, `"method"`) && !strings.Contains(strings.Join(m.detailLines, "\n"), "method") {
		t.Fatalf("expected pretty json, got %q", strings.Join(m.detailLines, "\n"))
	}

	m.detailOff = 4
	m.cursor = len(m.filtered) - 1
	m.refreshDetail()
	full := m.View()
	if strings.Count(full, "json ·") > 0 && strings.Count(full, "log ·") > 0 {
		t.Fatalf("both json and log titles visible:\n%s", full)
	}
	body := strings.Join(m.detailLines, "\n")
	if strings.Contains(body, `"method"`) || strings.Contains(body, "response_code") {
		t.Fatalf("stale json left in detail after switching to a plain line:\n%s", body)
	}
	if !strings.Contains(body, "worker tick") {
		t.Fatalf("expected plain log body, got %q", body)
	}
	if m.detailJSON {
		t.Fatal("detail still marked as json")
	}
	if m.detailOff != 0 {
		t.Fatalf("detail scroll not reset: %d", m.detailOff)
	}
}

func TestFollowDoesNotStealDetailScroll(t *testing.T) {
	m := testModel(140, 40, true, true)
	m.follow = true
	m.paused = false
	m.focus = paneDetail
	m.cursor = 0
	m.refreshDetail()
	m.detailOff = 3
	held := m.cursor

	batch := make(LogBatchMsg, 0, 4)
	for i := 0; i < 4; i++ {
		line := fmt.Sprintf(`{"method":"GET","x-envoy-origin-path":"/n/%d","response_code":200}`, i)
		batch = append(batch, source.Event{
			Namespace: "echo",
			Pod:       "echo-new",
			Container: "echo",
			Line:      line,
		})
	}
	m.ingest(batch)
	if m.cursor != held {
		t.Fatalf("follow snapped cursor while detail focused: %d -> %d", held, m.cursor)
	}
	if m.detailOff != 3 {
		t.Fatalf("follow reset detail scroll: %d", m.detailOff)
	}

	m.focus = paneLogs
	m.ingest(batch)
	if m.cursor != len(m.filtered)-1 {
		t.Fatalf("log focus should still follow: cursor=%d last=%d", m.cursor, len(m.filtered)-1)
	}
}

func TestErrorsStayInBadgeNotFrame(t *testing.T) {
	m := testModel(140, 40, true, true)
	before := m.View()
	m.pushErr(time.Now(), "Waited for 1.17s due to client-side throttling, request: GET pods/log")
	m.pushErr(time.Now(), "Waited for 1.17s due to client-side throttling, request: GET pods/log")
	after := m.View()
	if lipgloss.Height(before) != lipgloss.Height(after) {
		t.Fatalf("error badge changed height %d -> %d", lipgloss.Height(before), lipgloss.Height(after))
	}
	if !strings.Contains(after, "error") {
		t.Fatalf("missing error badge:\n%s", after)
	}
	if strings.Contains(after, "request.go:") || strings.Contains(after, "Waited for") {
		t.Fatal("raw throttle line leaked into the main frame")
	}
	m.showErrs = true
	ov := m.View()
	if lipgloss.Height(ov) > 40 {
		t.Fatalf("error overlay taller than terminal: %d", lipgloss.Height(ov))
	}
	if !strings.Contains(ov, "throttling") {
		t.Fatal("overlay missing error text")
	}
}

func TestSkipANSIKeepsColor(t *testing.T) {
	s := "\x1b[31mGET\x1b[0m  \x1b[32m/very/long/path\x1b[0m"
	cut := skipANSICells(s, 5)
	if !strings.Contains(cut, "\x1b[") {
		t.Fatalf("expected ANSI to survive a horizontal skip: %q", cut)
	}
	if strings.Contains(cut, "GET") {
		t.Fatalf("should have skipped GET: %q", cut)
	}
	if !strings.Contains(cut, "/very/long/path") {
		t.Fatalf("expected remaining path, got %q", cut)
	}
}

func TestNamespacePickerFilters(t *testing.T) {
	m := testModel(140, 40, true, true)
	plain := `2026-08-14T21:00:01Z INFO worker tick n=3`
	ln := logLine{
		Ev:     source.Event{Namespace: "logs", Pod: "chatter-1", Container: "chatter", Line: plain},
		Rec:    parse.Line(plain),
		Source: "logs/chatter-1/chatter",
		Color:  colorFor("logs/chatter"),
	}
	m.lines = append(m.lines, ln)
	m.rememberNS("logs")
	m.rememberNS("echo")
	m.refilter()
	before := len(m.filtered)
	if before < 2 {
		t.Fatalf("expected mixed ns lines, got %d", before)
	}
	m.openNamespacePicker()
	if !m.showNS {
		t.Fatal("picker not open")
	}
	items := m.nsItems()
	// item 0 is all; find logs
	idx := -1
	for i, n := range items {
		if n == "logs" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("logs missing from picker: %v", items)
	}
	m.pickNamespace(idx)
	if m.nsOnly != "logs" {
		t.Fatalf("nsOnly=%q", m.nsOnly)
	}
	for _, i := range m.filtered {
		if m.lines[i].Ev.Namespace != "logs" {
			t.Fatalf("leaked ns %q", m.lines[i].Ev.Namespace)
		}
	}
	view := m.View()
	if lipgloss.Height(view) > 40 {
		t.Fatalf("picker/filter overflow %d", lipgloss.Height(view))
	}
	m.openNamespacePicker()
	ov := m.View()
	if lipgloss.Height(ov) > 40 {
		t.Fatalf("picker overlay overflow %d", lipgloss.Height(ov))
	}
	if !strings.Contains(ov, "logs") || !strings.Contains(ov, "all namespaces") {
		t.Fatalf("picker missing items:\n%s", ov)
	}
}

func TestLayoutTiles(t *testing.T) {
	m := testModel(140, 40, true, true)
	ly := m.layout()
	if ly.srcH != ly.bodyH {
		t.Fatalf("sources height %d != body %d", ly.srcH, ly.bodyH)
	}
	if ly.logH+ly.detH != ly.bodyH {
		t.Fatalf("logs %d + detail %d != body %d", ly.logH, ly.detH, ly.bodyH)
	}
	if ly.srcW+ly.logW != ly.bodyW {
		t.Fatalf("src %d + logs %d != bodyW %d", ly.srcW, ly.logW, ly.bodyW)
	}
}
