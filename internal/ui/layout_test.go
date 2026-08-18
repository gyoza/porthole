package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	if strings.Contains(full, "[json]") && strings.Contains(full, "[raw]") {
		t.Fatalf("both [json] and [raw] titles visible:\n%s", full)
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

func TestFollowKeyToggles(t *testing.T) {
	m := testModel(140, 40, true, true)
	m.follow = true
	m.focus = paneLogs
	m.cursor = 0
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if m.follow {
		t.Fatal("f should unfollow")
	}
	if m.cursor != 0 {
		t.Fatalf("unfollow should not jump, cursor=%d", m.cursor)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if !m.follow {
		t.Fatal("f again should follow")
	}
	if m.cursor != len(m.filtered)-1 {
		t.Fatalf("follow should jump to tail, cursor=%d last=%d", m.cursor, len(m.filtered)-1)
	}
}

func TestLogsHideTimestampByDefault(t *testing.T) {
	m := testModel(140, 40, true, true)
	ly := m.layout()
	logs := m.logsView(ly)
	if strings.Contains(logs, "03:47:10") {
		t.Fatalf("[logs] should hide parsed clock by default:\n%s", logs)
	}
	det := m.detailView(ly)
	if !strings.Contains(det, "03:47:10") {
		t.Fatalf("[json]/[raw] title should still show the clock:\n%s", det)
	}
	m.showTime = true
	if !strings.Contains(m.logsView(ly), "03:47:10") {
		t.Fatal("t / --timestamps should show clock in [logs]")
	}
}

func TestPlainLineHidesLeadingStamp(t *testing.T) {
	m := New(Options{}).(*model)
	m.width, m.height = 140, 40
	m.showDetail = true
	m.ingest(LogBatchMsg{{
		Namespace: "logs", Pod: "chatter-1", Container: "chatter",
		Line: "Fri, 14 Aug 2026 18:26:46 GMT | [GET] - http://10.244.0.11:80/",
	}})
	logs := m.logsView(m.layout())
	if strings.Contains(logs, "18:26:46") || strings.Contains(logs, "2026") {
		t.Fatalf("[logs] still has a clock:\n%s", logs)
	}
	if !strings.Contains(logs, "[GET]") || !strings.Contains(logs, "10.244.0.11") {
		t.Fatalf("expected request, got:\n%s", logs)
	}
}

func TestFollowDoesNotStealDetailScroll(t *testing.T) {
	m := testModel(140, 40, true, true)
	m.follow = true
	m.paused = false
	m.focus = paneDetail
	m.cursor = 0
	m.refreshDetail()
	held := m.cursor
	first, _ := m.selected()

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
	got, ok := m.selected()
	if !ok || got.Ev.Line != first.Ev.Line {
		t.Fatal("detail selection changed while follow ingested")
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

func TestPaneKeepsPaintColors(t *testing.T) {
	m := testModel(80, 24, false, false)
	body := "\x1b[32mGET\x1b[0m  /ok  \x1b[31m500\x1b[0m"
	got := m.pane("logs", "", true, 40, 6, body)
	if !strings.Contains(got, "\x1b[32mGET\x1b[0m") || !strings.Contains(got, "\x1b[31m500\x1b[0m") {
		t.Fatalf("pane stripped log colors:\n%q", got)
	}
}

func TestSelectedCopyUsesPretty(t *testing.T) {
	m := testModel(140, 40, true, true)
	text, ok := m.selectedCopy()
	if !ok {
		t.Fatal("expected a selected line")
	}
	if !strings.Contains(text, `"method"`) && !strings.Contains(text, "GET") {
		t.Fatalf("copy text should be raw/pretty JSON, got %q", text[:min(80, len(text))])
	}
}

func TestRegexMatchHasDarkOnGold(t *testing.T) {
	st := defaultTheme().match()
	got := st.Render("500")
	if !strings.Contains(got, "500") {
		t.Fatalf("match style dropped text: %q", got)
	}
	// lipgloss only emits CSI when it thinks the output is a TTY;
	// the important contract is we do not inherit muted grey onto gold.
	th := defaultTheme()
	if st.GetForeground() == th.muted {
		t.Fatal("match fg must not be muted grey")
	}
	if st.GetForeground() != th.matchFg {
		t.Fatalf("match fg=%v want %v", st.GetForeground(), th.matchFg)
	}
	if st.GetBackground() != th.matchBg {
		t.Fatalf("match bg=%v want %v", st.GetBackground(), th.matchBg)
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

func TestIncludeSeedsLiveFilter(t *testing.T) {
	const id = "6ef0ac35-0794-46fe-bec6-c6d89a420a29"
	m := New(Options{Include: id}).(*model)
	if m.input.Value() != id {
		t.Fatalf("filter box=%q", m.input.Value())
	}
	hit := `{"level":"info","msg":"found","trace_id":"` + id + `"}`
	miss := `{"level":"info","msg":"nope","trace_id":"other"}`
	m.ingest(LogBatchMsg{
		{Namespace: "ns", Pod: "p", Container: "c", Line: hit},
		{Namespace: "ns", Pod: "p", Container: "c", Line: miss},
	})
	if len(m.filtered) != 1 {
		t.Fatalf("filtered=%d want 1 (uuid include)", len(m.filtered))
	}
	if !strings.Contains(m.lines[m.filtered[0]].Ev.Line, id) {
		t.Fatalf("kept the wrong line: %s", m.lines[m.filtered[0]].Ev.Line)
	}
}

func TestProgressCRDoesNotBreakSources(t *testing.T) {
	m := testModel(140, 40, true, true)
	progress := "  % Total    % Received % Xferd  Average Speed   Time    Time     Time  Current\r" +
		"                                 Dload  Upload   Total   Spent    Left  Speed\r" +
		"100 35589  0 35589    0     0   847k      0 --:--:-- --:--:-- --:--:--  847k"
	m.ingest(LogBatchMsg{{
		Namespace: "prod",
		Pod:       "sl-openbao-backup-29772830-lw4wc",
		Container: "awscli",
		Line:      progress,
	}})
	if strings.Contains(m.lines[len(m.lines)-1].Ev.Line, "\r") {
		t.Fatal("stored line still has carriage return")
	}
	v := m.View()
	if strings.Contains(v, "\r") {
		t.Fatal("view leaked \\r — would paint over [context]")
	}
	if !strings.Contains(v, "[context]") {
		t.Fatalf("missing [context] after progress line:\n%s", v)
	}
	if !strings.Contains(v, "35589") {
		t.Fatalf("expected last progress snapshot in view:\n%s", v)
	}
	if lipgloss.Width(v) > 140 || lipgloss.Height(v) > 40 {
		t.Fatalf("frame %dx%d after progress line", lipgloss.Width(v), lipgloss.Height(v))
	}
}

func TestRingBufferDoesNotPanicOnOverflow(t *testing.T) {
	m := New(Options{Include: "keep"}).(*model)
	m.width, m.height = 120, 40
	m.showDetail = true
	m.follow = false
	m.focus = paneDetail

	flush := func(batch []source.Event) {
		if len(batch) > 0 {
			m.ingest(batch)
		}
	}
	var batch []source.Event
	for i := 0; i < maxLines+80; i++ {
		line := "other"
		if i%17 == 0 {
			line = fmt.Sprintf("keep %d", i)
		}
		batch = append(batch, source.Event{
			Namespace: "ns", Pod: "p", Container: "c",
			Line: line,
		})
		if len(batch) == 64 {
			flush(batch)
			batch = batch[:0]
		}
	}
	flush(batch)
	if len(m.lines) > maxLines {
		t.Fatalf("lines=%d want <= %d", len(m.lines), maxLines)
	}
	if _, ok := m.selected(); ok {
		if m.filtered[m.cursor] >= len(m.lines) {
			t.Fatalf("stale filter index %d len=%d", m.filtered[m.cursor], len(m.lines))
		}
	}
	_ = m.View()
}

func TestSourcesSurviveRingWrap(t *testing.T) {
	m := New(Options{}).(*model)
	m.width, m.height = 120, 40
	quiet := source.Event{Namespace: "ns", Pod: "quiet", Container: "c", Line: "hello from quiet"}
	m.ingest(LogBatchMsg{quiet, quiet})
	var batch []source.Event
	for i := 0; i < maxLines+20; i++ {
		batch = append(batch, source.Event{
			Namespace: "ns", Pod: "noisy", Container: "c",
			Line: fmt.Sprintf("n %d", i),
		})
		if len(batch) == 64 {
			m.ingest(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		m.ingest(batch)
	}
	ids := map[string]int{}
	for _, s := range m.sources {
		ids[s.ID] = s.Count
	}
	if _, ok := ids["ns/quiet/c"]; !ok {
		t.Fatal("quiet pod disappeared from [context] after the ring wrapped")
	}
	if ids["ns/quiet/c"] != 0 {
		t.Fatalf("quiet in-window count=%d want 0", ids["ns/quiet/c"])
	}
	if ids["ns/noisy/c"] == 0 {
		t.Fatal("noisy pod should still have lines in the ring")
	}
	if len(m.sources) != 2 {
		t.Fatalf("sources=%d want 2 (insertion order, sticky): %+v", len(m.sources), m.sources)
	}
	if m.sources[0].ID != "ns/quiet/c" || m.sources[1].ID != "ns/noisy/c" {
		t.Fatalf("source order shuffled: %+v", m.sources)
	}
}

func TestDualContextSources(t *testing.T) {
	m := New(Options{Contexts: []string{"prod1", "prod2"}}).(*model)
	m.width, m.height = 140, 40
	m.showSources = true
	m.showDetail = true
	m.ingest(LogBatchMsg{
		{Context: "prod1", Namespace: "ns", Pod: "nginx-a", Container: "nginx", Line: `{"msg":"one"}`},
		{Context: "prod2", Namespace: "ns", Pod: "nginx-b", Container: "nginx", Line: `{"msg":"two"}`},
	})
	ly := m.layout()
	if ly.srcH2 == 0 || ly.srcH+ly.srcH2 != ly.bodyH {
		t.Fatalf("dual sources heights %d+%d body=%d", ly.srcH, ly.srcH2, ly.bodyH)
	}
	v := m.View()
	for _, name := range []string{"[context · prod1]", "[context · prod2]"} {
		if !strings.Contains(v, name) {
			t.Fatalf("missing %s in:\n%s", name, v)
		}
	}
	logs := m.logsView(ly)
	if !strings.Contains(logs, "prod1") || !strings.Contains(logs, "prod2") {
		t.Fatalf("[logs] should prefix context:\n%s", logs)
	}
	if !strings.Contains(logs, "nginx-a") || !strings.Contains(logs, "nginx-b") {
		t.Fatalf("[logs] missing pods:\n%s", logs)
	}
}

func TestSingleContextNamedOnBorder(t *testing.T) {
	m := New(Options{Context: "prod", Contexts: []string{"prod"}}).(*model)
	m.width, m.height = 140, 40
	m.showSources = true
	m.showDetail = true
	m.ingest(LogBatchMsg{{
		Context: "prod", Namespace: "ns", Pod: "nginx-a", Container: "nginx",
		Line: `{"msg":"one"}`,
	}})
	v := m.View()
	if !strings.Contains(v, "[context · prod]") {
		t.Fatalf("single context should name the pane:\n%s", v)
	}
	if strings.Contains(v, "[context]") && !strings.Contains(v, "[context · prod]") {
		t.Fatal("bare [context] without the name")
	}
	logs := m.logsView(m.layout())
	if !strings.Contains(logs, "prod") {
		t.Fatalf("[logs] should prefix the context:\n%s", logs)
	}
}

func TestPaneNamesOnBorder(t *testing.T) {
	m := testModel(140, 40, true, true)
	v := m.View()
	for _, name := range []string{"[context]", "[logs]"} {
		if !strings.Contains(v, name) {
			t.Fatalf("missing %s in:\n%s", name, v)
		}
	}
	if !strings.Contains(v, "[json]") && !strings.Contains(v, "[raw]") {
		t.Fatalf("missing [json]/[raw] in:\n%s", v)
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
