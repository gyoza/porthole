package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gyoza/porthole/internal/filter"
	"github.com/gyoza/porthole/internal/parse"
	"github.com/gyoza/porthole/internal/source"
)

const (
	maxLines = 20000
	helpText = `porthole — windowed log tailer

  /            focus live regex (pre-filled by -i/--include)
  enter        apply / leave filter
  esc          leave filter or close help
  tab          cycle panes
  j k ↑ ↓      move selection
  ← →          scroll a clipped log line
  g G          top / bottom
  pgup pgdn    page
  f            follow / unfollow the tail
  p            pause / resume ingest (stop new lines)
               (unfollow [logs] with f; follow does not steal [json]/[context])
  t            timestamps in [logs] (off by default; always on [json]/[raw])
  d            toggle [json]/[raw] pane
  s            toggle [context] pane (two panes when --context is repeated)
  y            copy selected [json]/[raw] to the clipboard
  x            export [logs] (sanitized, current filter) to a file
  X            export every raw line in memory to a file
  e            view client / tail errors
  n            choose namespace
  ?            this help
  q            quit

Regex is compiled as you type. An incomplete pattern
keeps the last valid filter so the stream stays visible.
JSON vs plain text is detected per line. There is no format flag.
Kubernetes client errors stay in the top-right badge, not the log stream.`
)

type pane int

const (
	paneLogs pane = iota
	paneSources
	paneSources2
	paneFilter
	paneDetail
)

// LogBatchMsg is a burst of ingested events from the tailer.
type LogBatchMsg []source.Event

// ErrMsg reports a fatal source error.
type ErrMsg struct{ Err error }

// DoneMsg means the source closed (EOF on a file, for example).
type DoneMsg struct{}

// StatusErrMsg is a client/tail error that must not enter the log stream.
type StatusErrMsg struct {
	Time time.Time
	Text string
}

type statusErr struct {
	Time  time.Time
	Text  string
	Count int
}

const maxStatusErrs = 80

type logLine struct {
	Ev     source.Event
	Rec    parse.Record
	Source string
	Color  lipgloss.Color
}

type srcStat struct {
	ID    string
	Color lipgloss.Color
	Count int
}

// Options configure the TUI session.
type Options struct {
	Title      string
	Context    string
	Contexts   []string
	Namespace  string
	Query      string
	Namespaces []string
	// Include is the initial live regex (Stern -i/--include).
	Include string
	// Exclude hides matching lines (Stern -e/--exclude). Not shown in / .
	Exclude string
	// ShowTime paints parsed clocks in [logs]. Off by default; [json]/[raw]
	// still shows the stamp on the title rule.
	ShowTime bool
	// SwitchNS retargets the cluster tailer. Empty means all namespaces.
	// Nil when the source is a file, stdin, or demo.
	SwitchNS func(string)
}

type model struct {
	opts   Options
	theme  theme
	width  int
	height int

	lines    []logLine
	filtered []int
	cursor   int
	offset   int
	logCol   int
	follow   bool
	paused   bool
	showTime bool
	eof      bool

	showDetail  bool
	showSources bool
	showHelp    bool
	focus       pane

	input   textinput.Model
	live    filter.Filter
	typed   filter.Filter
	exclude filter.Filter

	sources []srcStat
	srcIdx  map[string]int
	srcSel  int
	srcSel2 int
	srcOnly string

	nsOnly   string
	nsChosen bool
	showNS   bool
	nsSel    int
	nsKnown  []string

	detailLines []string
	detailOff   int
	detailKey   string
	detailJSON  bool

	errs     []statusErr
	showErrs bool
	errSel   int

	copiedN    int
	copiedAt   time.Time
	exportNote string
	exportAt   time.Time

	started time.Time
	err     error
}

// New returns the Bubble Tea model.
func New(opts Options) tea.Model {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "live regex — try 5[0-9]{2} or POST|/login"
	ti.CharLimit = 1024
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#5A6A80"))
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#98C1D9")).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E8EEF4"))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#EE6C4D"))

	if opts.Include != "" {
		ti.SetValue(opts.Include)
	}
	known := append([]string(nil), opts.Namespaces...)
	m := &model{
		opts:        opts,
		theme:       defaultTheme(),
		follow:      true,
		showTime:    opts.ShowTime,
		showDetail:  true,
		showSources: true,
		focus:       paneLogs,
		input:       ti,
		srcIdx:      map[string]int{},
		nsKnown:     known,
		started:     time.Now(),
	}
	if opts.Exclude != "" {
		m.exclude = filter.Compile(opts.Exclude)
	}
	if opts.Include != "" {
		m.applyFilter(opts.Include)
	}
	return m
}

func (m *model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()
		return m, nil

	case LogBatchMsg:
		if m.paused {
			return m, nil
		}
		m.ingest(msg)
		return m, nil

	case ErrMsg:
		m.err = msg.Err
		m.pushErr(time.Now(), msg.Err.Error())
		return m, nil

	case copiedMsg:
		m.copiedN = msg.n
		m.copiedAt = time.Now()
		return m, nil

	case exportedMsg:
		if msg.err != nil {
			m.pushErr(time.Now(), "export: "+msg.err.Error())
			return m, nil
		}
		m.exportNote = fmt.Sprintf("wrote %d  %s", msg.n, msg.path)
		m.exportAt = time.Now()
		return m, nil

	case StatusErrMsg:
		m.pushErr(msg.Time, msg.Text)
		return m, nil

	case DoneMsg:
		m.eof = true
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.focus == paneFilter {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.applyFilter(m.input.Value())
		return m, cmd
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		switch msg.String() {
		case "q", "esc", "?", "enter":
			m.showHelp = false
		}
		return m, nil
	}

	if m.showNS {
		switch msg.String() {
		case "n", "esc", "q":
			m.showNS = false
		case "j", "down":
			if m.nsSel < len(m.nsItems())-1 {
				m.nsSel++
			}
		case "k", "up":
			if m.nsSel > 0 {
				m.nsSel--
			}
		case "enter":
			m.pickNamespace(m.nsSel)
			m.showNS = false
		case "g", "home":
			m.nsSel = 0
		case "G", "end":
			if n := len(m.nsItems()); n > 0 {
				m.nsSel = n - 1
			}
		}
		return m, nil
	}

	if m.showErrs {
		switch msg.String() {
		case "e", "esc", "q", "enter":
			m.showErrs = false
		case "j", "down":
			if m.errSel < len(m.errs)-1 {
				m.errSel++
			}
		case "k", "up":
			if m.errSel > 0 {
				m.errSel--
			}
		case "c":
			m.errs = nil
			m.errSel = 0
			m.showErrs = false
		}
		return m, nil
	}

	if m.focus == paneDetail {
		switch msg.String() {
		case "j", "down":
			m.scrollDetail(1)
			return m, nil
		case "k", "up":
			m.scrollDetail(-1)
			return m, nil
		case "pgdown", "ctrl+d":
			m.scrollDetail(m.layout().detRows)
			return m, nil
		case "pgup", "ctrl+u":
			m.scrollDetail(-m.layout().detRows)
			return m, nil
		case "left", "h":
			m.scrollLogsH(-8)
			return m, nil
		case "right", "l":
			m.scrollLogsH(8)
			return m, nil
		}
	}

	if m.focus == paneSources || m.focus == paneSources2 {
		list, sel := m.sourceList(m.focus)
		switch msg.String() {
		case "j", "down":
			if *sel < len(list)-1 {
				(*sel)++
			}
			return m, nil
		case "k", "up":
			if *sel > 0 {
				(*sel)--
			}
			return m, nil
		}
	}

	if m.focus == paneFilter {
		switch msg.String() {
		case "esc":
			m.input.Blur()
			m.focus = paneLogs
			return m, nil
		case "enter":
			m.input.Blur()
			m.focus = paneLogs
			m.applyFilter(m.input.Value())
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.applyFilter(m.input.Value())
			return m, cmd
		}
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showHelp = true
	case "e":
		if len(m.errs) > 0 {
			m.showErrs = true
			if m.errSel >= len(m.errs) {
				m.errSel = len(m.errs) - 1
			}
		}
	case "n":
		m.openNamespacePicker()
	case "/":
		m.focus = paneFilter
		return m, m.input.Focus()
	case "esc":
		m.srcOnly = ""
		m.refilter()
	case "tab":
		m.cyclePane(1)
	case "shift+tab":
		m.cyclePane(-1)
	case "d":
		m.showDetail = !m.showDetail
		if !m.showDetail && m.focus == paneDetail {
			m.focus = paneLogs
		}
		m.relayout()
	case "s":
		m.showSources = !m.showSources
		if !m.showSources && (m.focus == paneSources || m.focus == paneSources2) {
			m.focus = paneLogs
		}
		m.relayout()
	case "p":
		m.paused = !m.paused
	case "f":
		m.follow = !m.follow
		if m.follow {
			m.jumpBottom()
		}
	case "t":
		m.showTime = !m.showTime
	case "y":
		if text, ok := m.selectedCopy(); ok {
			return m, copyToClipboard(text)
		}
	case "x":
		return m, writeExport("logs", m.exportSanitized())
	case "X":
		return m, writeExport("raw", m.exportRaw())
	case "g", "home":
		m.follow = false
		m.cursor = 0
		m.offset = 0
	case "G", "end":
		m.jumpBottom()
	case "left", "h":
		m.scrollLogsH(-8)
	case "right", "l":
		m.scrollLogsH(8)
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "pgdown", "ctrl+d":
		m.move(m.logHeight())
	case "pgup", "ctrl+u":
		m.move(-m.logHeight())
	case "enter":
		if m.focus == paneSources || m.focus == paneSources2 {
			list, sel := m.sourceList(m.focus)
			if *sel >= 0 && *sel < len(list) {
				id := list[*sel].ID
				if m.srcOnly == id {
					m.srcOnly = ""
				} else {
					m.srcOnly = id
				}
				m.refilter()
			}
		} else {
			m.showDetail = !m.showDetail
			m.relayout()
		}
	}
	m.refreshDetail()
	return m, nil
}

func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			// Heuristic: bottom two rows are filter/footer.
			if msg.Y >= m.height-2 {
				m.focus = paneFilter
				return m, m.input.Focus()
			}
		}
	case tea.MouseActionMotion:
		// ignore
	}
	if msg.Button == tea.MouseButtonWheelUp {
		m.wheel(-3)
	}
	if msg.Button == tea.MouseButtonWheelDown {
		m.wheel(3)
	}
	return m, nil
}

func (m *model) wheel(delta int) {
	switch m.focus {
	case paneDetail:
		m.scrollDetail(delta)
	case paneSources, paneSources2:
		list, sel := m.sourceList(m.focus)
		*sel += delta
		if *sel < 0 {
			*sel = 0
		}
		if *sel >= len(list) {
			*sel = len(list) - 1
		}
	default:
		m.move(delta)
		m.refreshDetail()
	}
}

func (m *model) cyclePane(dir int) {
	order := []pane{paneLogs}
	if m.showSources {
		order = append(order, paneSources)
		if m.dualContext() {
			order = append(order, paneSources2)
		}
	}
	if m.showDetail {
		order = append(order, paneDetail)
	}
	order = append(order, paneFilter)
	idx := 0
	for i, p := range order {
		if p == m.focus {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(order)) % len(order)
	m.focus = order[idx]
	if m.focus == paneFilter {
		_ = m.input.Focus()
	} else {
		m.input.Blur()
	}
}

func (m *model) ingest(batch []source.Event) {
	for _, ev := range batch {
		ev.Line = parse.Sanitize(ev.Line)
		ln := logLine{
			Ev:     ev,
			Rec:    parse.Line(ev.Line),
			Source: ev.SourceID(),
			Color:  colorFor(ev.ColorSeed()),
		}
		m.lines = append(m.lines, ln)
		m.bumpSource(ln)
		m.rememberNS(ev.Namespace)
		if m.keepLine(ln) {
			m.filtered = append(m.filtered, len(m.lines)-1)
		}
	}
	if len(m.lines) > maxLines {
		n := len(m.lines) - maxLines
		for _, ln := range m.lines[:n] {
			m.dropSourceLine(ln)
		}
		m.lines = append([]logLine(nil), m.lines[n:]...)
		// filtered still holds pre-trim indices; selected() would panic
		// (index 20044 of a 20000-line ring) if refilter pinned first.
		m.filtered = m.filtered[:0]
		m.refilter()
	}
	if m.viewFollowsTail() && len(m.filtered) > 0 {
		m.cursor = len(m.filtered) - 1
		m.ensureVisible()
	}
	m.refreshDetail()
}

// viewFollowsTail reports whether ingest should snap the log cursor (and
// thus the detail pane) to the newest line. Following still appends
// lines; only the live log list is pinned to the tail. Detail, sources,
// and overlays keep their selection so they stay scrollable.
func (m *model) viewFollowsTail() bool {
	if !m.follow || m.paused {
		return false
	}
	if m.showHelp || m.showErrs || m.showNS {
		return false
	}
	return m.focus == paneLogs || m.focus == paneFilter
}

func (m *model) dualContext() bool {
	return len(m.opts.Contexts) >= 2
}

func (m *model) sourceList(p pane) ([]srcStat, *int) {
	if !m.dualContext() {
		return m.sources, &m.srcSel
	}
	idx, sel := 0, &m.srcSel
	if p == paneSources2 {
		idx = 1
		sel = &m.srcSel2
	}
	if idx >= len(m.opts.Contexts) {
		return nil, sel
	}
	return m.sourcesFor(m.opts.Contexts[idx]), sel
}

func (m *model) sourcesFor(ctx string) []srcStat {
	out := make([]srcStat, 0, len(m.sources))
	for _, s := range m.sources {
		if sourceContext(s.ID) == ctx {
			out = append(out, s)
		}
	}
	return out
}

func (m *model) bumpSource(ln logLine) {
	if i, ok := m.srcIdx[ln.Source]; ok {
		m.sources[i].Count++
		return
	}
	m.srcIdx[ln.Source] = len(m.sources)
	m.sources = append(m.sources, srcStat{ID: ln.Source, Color: ln.Color, Count: 1})
}

// dropSourceLine decrements the in-window count when a line leaves the
// ring. The source itself stays so [context] does not forget a pod just
// because noisier pods pushed its lines out.
func (m *model) dropSourceLine(ln logLine) {
	i, ok := m.srcIdx[ln.Source]
	if !ok {
		return
	}
	if m.sources[i].Count > 0 {
		m.sources[i].Count--
	}
}

func (m *model) keepLine(ln logLine) bool {
	if m.srcOnly != "" && ln.Source != m.srcOnly {
		return false
	}
	if m.nsOnly != "" && ln.Ev.Namespace != m.nsOnly {
		return false
	}
	if !m.live.Match(ln.Rec, ln.Source) {
		return false
	}
	if m.exclude.Regexp != nil && m.exclude.Match(ln.Rec, ln.Source) {
		return false
	}
	return true
}

func (m *model) rememberNS(ns string) {
	if ns == "" {
		return
	}
	for _, n := range m.nsKnown {
		if n == ns {
			return
		}
	}
	m.nsKnown = append(m.nsKnown, ns)
}

func (m *model) nsItems() []string {
	seen := map[string]struct{}{}
	items := make([]string, 0, len(m.nsKnown)+1)
	items = append(items, "")
	for _, n := range m.nsKnown {
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		items = append(items, n)
	}
	sort.Strings(items[1:])
	return items
}

func (m *model) nsCounts() map[string]int {
	c := map[string]int{}
	for _, ln := range m.lines {
		if ln.Ev.Namespace != "" {
			c[ln.Ev.Namespace]++
		}
	}
	return c
}

func (m *model) openNamespacePicker() {
	m.showNS = true
	items := m.nsItems()
	m.nsSel = 0
	for i, n := range items {
		if n == m.nsOnly {
			m.nsSel = i
			break
		}
	}
}

func (m *model) pickNamespace(idx int) {
	items := m.nsItems()
	if idx < 0 || idx >= len(items) {
		return
	}
	m.nsOnly = items[idx]
	m.nsChosen = true
	m.srcOnly = ""
	m.refilter()
	if m.opts.SwitchNS != nil {
		m.opts.SwitchNS(m.nsOnly)
	}
}

func (m *model) applyFilter(pattern string) {
	typed := filter.Compile(pattern)
	m.typed = typed
	if typed.Valid() {
		m.live = typed
		m.refilter()
	}
}

func (m *model) refilter() {
	pin := ""
	if !m.viewFollowsTail() {
		if ln, ok := m.selected(); ok {
			pin = ln.Source + "\x00" + ln.Ev.Line
		}
	}
	m.filtered = m.filtered[:0]
	for i, ln := range m.lines {
		if m.keepLine(ln) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.viewFollowsTail() && len(m.filtered) > 0 {
		m.cursor = len(m.filtered) - 1
	} else if pin != "" {
		m.cursor = 0
		for i, idx := range m.filtered {
			ln := m.lines[idx]
			if ln.Source+"\x00"+ln.Ev.Line == pin {
				m.cursor = i
				break
			}
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.ensureVisible()
	m.refreshDetail()
}

func (m *model) scrollLogsH(delta int) {
	m.logCol += delta
	if m.logCol < 0 {
		m.logCol = 0
	}
	if m.logCol > 500 {
		m.logCol = 500
	}
}

func (m *model) move(delta int) {
	if len(m.filtered) == 0 {
		return
	}
	m.follow = false
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	m.ensureVisible()
}

func (m *model) jumpBottom() {
	if len(m.filtered) == 0 {
		return
	}
	m.cursor = len(m.filtered) - 1
	m.ensureVisible()
}

func (m *model) ensureVisible() {
	h := m.logHeight()
	if h <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *model) selected() (logLine, bool) {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return logLine{}, false
	}
	idx := m.filtered[m.cursor]
	if idx < 0 || idx >= len(m.lines) {
		return logLine{}, false
	}
	return m.lines[idx], true
}

func (m *model) refreshDetail() {
	ln, ok := m.selected()
	if !ok {
		m.detailLines = nil
		m.detailOff = 0
		m.detailKey = ""
		m.detailJSON = false
		return
	}
	key := fmt.Sprintf("%d:%s", m.filtered[m.cursor], ln.Source)
	isJSON := len(ln.Rec.JSONBytes) > 0
	width := max(8, m.layout().detW-4)
	m.detailLines = strings.Split(wrapWidth(ln.Rec.Pretty(), width), "\n")
	if key != m.detailKey || isJSON != m.detailJSON {
		m.detailOff = 0
	}
	m.detailKey = key
	m.detailJSON = isJSON
	m.clampDetail()
}

func (m *model) scrollDetail(delta int) {
	m.detailOff += delta
	m.clampDetail()
}

func (m *model) clampDetail() {
	vis := m.layout().detRows
	maxOff := len(m.detailLines) - vis
	if maxOff < 0 {
		maxOff = 0
	}
	if m.detailOff > maxOff {
		m.detailOff = maxOff
	}
	if m.detailOff < 0 {
		m.detailOff = 0
	}
}

func (m *model) pushErr(t time.Time, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if t.IsZero() {
		t = time.Now()
	}
	if n := len(m.errs); n > 0 && m.errs[n-1].Text == text {
		m.errs[n-1].Count++
		m.errs[n-1].Time = t
		return
	}
	m.errs = append(m.errs, statusErr{Time: t, Text: text, Count: 1})
	if len(m.errs) > maxStatusErrs {
		m.errs = append([]statusErr(nil), m.errs[len(m.errs)-maxStatusErrs:]...)
	}
	m.errSel = len(m.errs) - 1
}

func (m *model) relayout() {
	m.input.Width = max(10, m.width-24)
	m.ensureVisible()
	m.refreshDetail()
}

func (m *model) logHeight() int {
	return m.layout().logRows
}

// frame is a pixel-perfect tile map for one terminal size.
// Every pane size includes its border; the four rows header/body/filter/footer
// sum to m.height and the columns sum to m.width so toggling detail cannot
// push the header off-screen.
type frame struct {
	bodyW, bodyH    int
	srcW, srcH      int
	srcH2, srcRows2 int
	logW, logH      int
	detW, detH      int
	logRows         int
	detRows         int
	srcRows         int
}

func (m *model) layout() frame {
	var f frame
	f.bodyW = max(20, m.width)
	f.bodyH = m.height - 3 // header + filter + footer
	if f.bodyH < 8 {
		f.bodyH = 8
	}

	if m.showSources && m.width >= 100 {
		f.srcW = 28
		if m.width < 120 {
			f.srcW = 24
		}
		f.srcH = f.bodyH
		f.srcRows = max(1, f.srcH-2)
		if m.dualContext() {
			f.srcH = f.bodyH / 2
			f.srcH2 = f.bodyH - f.srcH
			f.srcRows = max(1, f.srcH-2)
			f.srcRows2 = max(1, f.srcH2-2)
		}
	}

	rightW := f.bodyW - f.srcW
	f.logW = rightW
	if m.showDetail && f.bodyH >= 12 {
		f.detW = rightW
		f.detH = f.bodyH / 3
		if f.detH < 8 {
			f.detH = 8
		}
		if f.detH > 18 {
			f.detH = 18
		}
		if f.detH > f.bodyH-7 {
			f.detH = f.bodyH - 7
		}
		f.logH = f.bodyH - f.detH
		f.detRows = max(1, f.detH-2)
	} else {
		f.logH = f.bodyH
	}
	f.logRows = max(1, f.logH-2)
	return f
}

func (m *model) matchCount() int { return len(m.filtered) }

func (m *model) headerText() string {
	bits := []string{"porthole"}
	if m.opts.Context != "" {
		bits = append(bits, "ctx="+m.opts.Context)
	}
	switch {
	case m.nsOnly != "":
		bits = append(bits, "ns="+m.nsOnly)
	case m.nsChosen:
		bits = append(bits, "ns=*")
	case m.opts.Namespace != "":
		bits = append(bits, "ns="+m.opts.Namespace)
	case m.opts.Title != "":
		bits = append(bits, m.opts.Title)
	}
	if m.opts.Query != "" && m.opts.Query != ".*" {
		bits = append(bits, "pod="+m.opts.Query)
	}
	bits = append(bits, fmt.Sprintf("%d src", len(m.sources)))
	bits = append(bits, fmt.Sprintf("%s / %s", comma(len(m.filtered)), comma(len(m.lines))))
	switch {
	case m.paused:
		bits = append(bits, "PAUSED")
	case m.eof:
		bits = append(bits, "EOF")
	case m.follow:
		bits = append(bits, "LIVE (Following)")
	default:
		bits = append(bits, "LIVE")
	}
	return strings.Join(bits, "  ·  ")
}

func comma(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 1000 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre == 0 {
		pre = 3
	}
	b.WriteString(s[:pre])
	for i := pre; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
