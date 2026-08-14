package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gyoza/porthole/internal/filter"
	"github.com/gyoza/porthole/internal/parse"
	"github.com/gyoza/porthole/internal/source"
)

const (
	maxLines = 20000
	helpText = `porthole — windowed log tailer

  /            focus live regex
  enter        apply / leave filter
  esc          leave filter or close help
  tab          cycle panes
  j k ↑ ↓      move selection
  g G          top / bottom
  pgup pgdn    page
  f            follow tail
  p            pause / resume ingest
  d            toggle JSON detail
  s            toggle source list
  ?            this help
  q            quit

Regex is compiled as you type. An incomplete pattern
keeps the last valid filter so the stream stays visible.
JSON from Envoy Gateway, zap, slog, and friends is
detected automatically (including a text prefix).`
)

type pane int

const (
	paneLogs pane = iota
	paneSources
	paneFilter
	paneDetail
)

// LogBatchMsg is a burst of ingested events from the tailer.
type LogBatchMsg []source.Event

// ErrMsg reports a fatal source error.
type ErrMsg struct{ Err error }

// DoneMsg means the source closed (EOF on a file, for example).
type DoneMsg struct{}

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
	Title     string
	Context   string
	Namespace string
	Query     string
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
	follow   bool
	paused   bool
	eof      bool

	showDetail  bool
	showSources bool
	showHelp    bool
	focus       pane

	input textinput.Model
	live  filter.Filter
	typed filter.Filter

	sources []srcStat
	srcIdx  map[string]int
	srcSel  int
	srcOnly string

	detail viewport.Model

	started time.Time
	err     error
}

// New returns the Bubble Tea model.
func New(opts Options) tea.Model {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "live regex — try 5[0-9]{2} or POST|/login"
	ti.CharLimit = 256
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#5A6A80"))
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#98C1D9")).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E8EEF4"))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#EE6C4D"))

	return &model{
		opts:        opts,
		theme:       defaultTheme(),
		follow:      true,
		showDetail:  true,
		showSources: true,
		focus:       paneLogs,
		input:       ti,
		srcIdx:      map[string]int{},
		started:     time.Now(),
		detail:      viewport.New(0, 0),
	}
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
	if m.focus == paneDetail {
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
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

	if m.focus == paneSources {
		switch msg.String() {
		case "j", "down":
			if m.srcSel < len(m.sources)-1 {
				m.srcSel++
			}
			return m, nil
		case "k", "up":
			if m.srcSel > 0 {
				m.srcSel--
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
		m.relayout()
	case "s":
		m.showSources = !m.showSources
		m.relayout()
	case "p":
		m.paused = !m.paused
	case "f":
		m.follow = true
		m.jumpBottom()
	case "g", "home":
		m.follow = false
		m.cursor = 0
		m.offset = 0
	case "G", "end":
		m.jumpBottom()
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "pgdown", "ctrl+d":
		m.move(m.logHeight())
	case "pgup", "ctrl+u":
		m.move(-m.logHeight())
	case "enter":
		if m.focus == paneSources && m.srcSel >= 0 && m.srcSel < len(m.sources) {
			id := m.sources[m.srcSel].ID
			if m.srcOnly == id {
				m.srcOnly = ""
			} else {
				m.srcOnly = id
			}
			m.refilter()
		} else {
			m.showDetail = !m.showDetail
			m.relayout()
		}
	default:
		if m.focus == paneDetail {
			var cmd tea.Cmd
			m.detail, cmd = m.detail.Update(msg)
			return m, cmd
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
		m.move(-3)
		m.refreshDetail()
	}
	if msg.Button == tea.MouseButtonWheelDown {
		m.move(3)
		m.refreshDetail()
	}
	return m, nil
}

func (m *model) cyclePane(dir int) {
	order := []pane{paneLogs}
	if m.showSources {
		order = append(order, paneSources)
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
		ln := logLine{
			Ev:     ev,
			Rec:    parse.Line(ev.Line),
			Source: ev.SourceID(),
			Color:  colorFor(ev.ColorSeed()),
		}
		m.lines = append(m.lines, ln)
		m.bumpSource(ln)
		if m.live.Match(ln.Rec, ln.Source) && (m.srcOnly == "" || m.srcOnly == ln.Source) {
			m.filtered = append(m.filtered, len(m.lines)-1)
		}
	}
	if len(m.lines) > maxLines {
		dropped := len(m.lines) - maxLines
		m.lines = append([]logLine(nil), m.lines[dropped:]...)
		m.rebuildSources()
		m.refilter()
	}
	if m.follow && len(m.filtered) > 0 {
		m.cursor = len(m.filtered) - 1
		m.ensureVisible()
	}
	m.refreshDetail()
}

func (m *model) rebuildSources() {
	m.sources = m.sources[:0]
	m.srcIdx = map[string]int{}
	for _, ln := range m.lines {
		m.bumpSource(ln)
	}
}

func (m *model) bumpSource(ln logLine) {
	if i, ok := m.srcIdx[ln.Source]; ok {
		m.sources[i].Count++
		return
	}
	m.srcIdx[ln.Source] = len(m.sources)
	m.sources = append(m.sources, srcStat{ID: ln.Source, Color: ln.Color, Count: 1})
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
	m.filtered = m.filtered[:0]
	for i, ln := range m.lines {
		if m.srcOnly != "" && ln.Source != m.srcOnly {
			continue
		}
		if m.live.Match(ln.Rec, ln.Source) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.follow && len(m.filtered) > 0 {
		m.cursor = len(m.filtered) - 1
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
	return m.lines[m.filtered[m.cursor]], true
}

func (m *model) refreshDetail() {
	ln, ok := m.selected()
	if !ok {
		m.detail.SetContent("")
		return
	}
	m.detail.SetContent(ln.Rec.Pretty())
}

func (m *model) relayout() {
	_, _, dw, dh := m.geom()
	m.detail.Width = dw
	m.detail.Height = dh
	m.input.Width = max(10, m.width-28)
	m.ensureVisible()
	m.refreshDetail()
}

func (m *model) logHeight() int {
	_, lh, _, _ := m.geom()
	return lh
}

func (m *model) geom() (srcW, logH, detailW, detailH int) {
	// header + filter + footer
	inner := m.height - 3
	if inner < 4 {
		inner = 4
	}
	srcW = 0
	if m.showSources && m.width >= 100 {
		srcW = 30
	}
	detailH = 0
	detailW = m.width
	if srcW > 0 {
		detailW = m.width - srcW - 1
	}
	if m.showDetail && inner >= 10 {
		detailH = inner / 3
		if detailH < 6 {
			detailH = 6
		}
		if detailH > 18 {
			detailH = 18
		}
	}
	logH = inner - detailH
	if logH < 3 {
		logH = 3
	}
	// account for box borders on logs/detail
	if logH > 2 {
		logH -= 2
	}
	if detailH > 2 {
		detailH -= 2
	}
	if detailW > 2 {
		detailW -= 2
	}
	return srcW, logH, detailW, detailH
}

func (m *model) matchCount() int { return len(m.filtered) }

func (m *model) headerText() string {
	bits := []string{"porthole"}
	if m.opts.Context != "" {
		bits = append(bits, "ctx="+m.opts.Context)
	}
	if m.opts.Namespace != "" {
		bits = append(bits, "ns="+m.opts.Namespace)
	} else if m.opts.Title != "" {
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
