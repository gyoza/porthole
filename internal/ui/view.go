package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/gyoza/porthole/internal/parse"
)

func (m *model) View() string {
	if m.width == 0 {
		return "starting porthole…"
	}
	if m.showHelp {
		return clipFrame(m.helpView(), m.width, m.height)
	}
	if m.showNS {
		return clipFrame(m.nsView(), m.width, m.height)
	}
	if m.showErrs {
		return clipFrame(m.errView(), m.width, m.height)
	}

	ly := m.layout()
	header := m.headerView()
	body := m.bodyView(ly)
	filter := m.filterView()
	footer := m.fitLine(m.theme.footer(), m.footerText())

	return clipFrame(lipgloss.JoinVertical(lipgloss.Left, header, body, filter, footer), m.width, m.height)
}

func clipFrame(s string, w, h int) string {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return lipgloss.NewStyle().Width(w).Height(h).MaxWidth(w).MaxHeight(h).Render(s)
}

func (m *model) fitLine(st lipgloss.Style, text string) string {
	return st.Width(max(1, m.width)).MaxHeight(1).Render(truncate(text, max(1, m.width-2)))
}

func (m *model) pane(title string, active bool, w, h int, body string) string {
	if w < 2 || h < 2 {
		return ""
	}
	innerW, innerH := w-2, h-2
	content := m.theme.title(active).Render(truncate(title, max(1, innerW-2))) + "\n" + body
	content = clipLines(content, innerH)
	return m.theme.borderBox(active).
		Width(innerW).
		Height(innerH).
		MaxWidth(w).
		MaxHeight(h).
		Render(content)
}

func clipLines(s string, n int) string {
	if n < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m *model) headerView() string {
	left := truncate(m.headerText(), max(8, m.width-28))
	right := ""
	if n := len(m.errs); n > 0 {
		label := fmt.Sprintf("%d error", n)
		if n != 1 {
			label += "s"
		}
		label += "  e"
		right = m.theme.error().Bold(true).Render(label)
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	return m.theme.header().Width(max(1, m.width)).MaxHeight(1).Render(line)
}

func (m *model) bodyView(ly frame) string {
	logs := m.logsView(ly)
	right := logs
	if ly.detH > 0 {
		right = lipgloss.JoinVertical(lipgloss.Left, logs, m.detailView(ly))
	}
	if ly.srcW > 0 {
		return lipgloss.JoinHorizontal(lipgloss.Top, m.sourcesView(ly), right)
	}
	return right
}

func (m *model) logsView(ly frame) string {
	rows := make([]string, 0, ly.logRows)
	if len(m.filtered) == 0 {
		msg := "waiting for logs…"
		if len(m.lines) > 0 {
			msg = "no lines match the current regex"
		}
		if m.err != nil {
			msg = m.err.Error()
		}
		rows = append(rows, m.theme.dim().Render("  "+msg))
	} else {
		end := m.offset + ly.logRows
		if end > len(m.filtered) {
			end = len(m.filtered)
		}
		innerW := max(8, ly.logW-4)
		for i := m.offset; i < end; i++ {
			ln := m.lines[m.filtered[i]]
			rows = append(rows, m.renderLine(ln, innerW, i == m.cursor))
		}
	}
	for len(rows) < ly.logRows {
		rows = append(rows, "")
	}
	title := "logs"
	if m.logCol > 0 {
		title = fmt.Sprintf("logs  ← %d", m.logCol)
	}
	return m.pane(title, m.focus == paneLogs, ly.logW, ly.logH, strings.Join(rows, "\n"))
}

func (m *model) renderLine(ln logLine, width int, selected bool) string {
	base := lipgloss.NewStyle().Foreground(m.theme.fg)
	if selected {
		base = m.theme.selected()
	}

	src := shortSource(ln.Source)
	srcSt := lipgloss.NewStyle().Foreground(ln.Color)
	if selected {
		srcSt = srcSt.Background(m.theme.selBg)
	}
	prefix := srcSt.Render(fmt.Sprintf("%-22s", truncate(src, 22)))

	var body string
	switch ln.Rec.Kind {
	case parse.KindHTTP:
		body = paintHTTP(m.theme, ln.Rec, base)
	case parse.KindApp:
		body = paintApp(m.theme, ln.Rec, base)
	default:
		body = base.Render(ln.Rec.Display)
	}
	if re := m.live.Regexp; re != nil {
		hi := lipgloss.NewStyle().Foreground(lipgloss.Color("#1B2838")).Background(m.theme.warn)
		switch {
		case re.MatchString(ln.Rec.Display):
			body = highlight(ln.Rec.Display, re, base, hi)
		case ln.Rec.Status > 0 && m.live.Match(ln.Rec, ln.Source):
			if stRe, err := regexp.Compile(`\b` + strconv.Itoa(ln.Rec.Status) + `\b`); err == nil && stRe.MatchString(ln.Rec.Display) {
				body = highlight(ln.Rec.Display, stRe, base, hi)
			}
		}
	}
	if m.logCol > 0 {
		body = skipANSICells(body, m.logCol)
	}

	line := prefix + " " + body
	if selected {
		line = m.theme.selected().Render("▸ ") + line
	} else {
		line = "  " + line
	}
	return truncatePlain(line, width)
}

// skipANSICells drops the first n printable cells but keeps escape
// sequences so lipgloss colors stay in effect on the remainder.
func skipANSICells(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	var b strings.Builder
	skipped := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		if s[i] == 0x1b {
			b.WriteByte(s[i])
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		if size < 1 {
			size = 1
		}
		if skipped < n {
			skipped++
			i += size
			continue
		}
		b.WriteString(s[i : i+size])
		i += size
	}
	return b.String()
}

func (m *model) sourcesView(ly frame) string {
	rows := make([]string, 0, len(m.sources))
	for i, s := range m.sources {
		mark := " "
		if s.ID == m.srcOnly {
			mark = "●"
		}
		label := fmt.Sprintf("%s %-16s %5d", mark, truncate(shortSource(s.ID), 16), s.Count)
		st := lipgloss.NewStyle().Foreground(s.Color)
		if i == m.srcSel && m.focus == paneSources {
			st = st.Background(m.theme.selBg).Bold(true)
		}
		rows = append(rows, st.Render(truncate(label, max(4, ly.srcW-4))))
	}
	if len(rows) == 0 {
		rows = append(rows, m.theme.dim().Render("  (none yet)"))
	}
	if start := srcWindow(m.srcSel, len(rows), ly.srcRows); start > 0 {
		rows = rows[start:]
	}
	return m.pane("sources", m.focus == paneSources, ly.srcW, ly.srcH, strings.Join(rows, "\n"))
}

func srcWindow(sel, n, avail int) int {
	if n <= avail || avail <= 0 {
		return 0
	}
	start := sel - avail/2
	if start < 0 {
		start = 0
	}
	if start > n-avail {
		start = n - avail
	}
	return start
}

func (m *model) detailView(ly frame) string {
	title := "detail"
	if ln, ok := m.selected(); ok {
		kind := "log"
		if len(ln.Rec.JSONBytes) > 0 {
			kind = "json"
		}
		title = truncate(kind+" · "+shortSource(ln.Source), max(4, ly.detW-6))
	}
	rows := ly.detRows
	start := m.detailOff
	if start > len(m.detailLines) {
		start = 0
	}
	end := start + rows
	if end > len(m.detailLines) {
		end = len(m.detailLines)
	}
	var body []string
	if start < end {
		body = append(body, m.detailLines[start:end]...)
	}
	for len(body) < rows {
		body = append(body, "")
	}
	return m.pane(title, m.focus == paneDetail, ly.detW, ly.detH, strings.Join(body, "\n"))
}

// wrapWidth hard-wraps s to width cells so the detail viewport's line
// count matches what is drawn (JSON pretty vs one long nginx line).
func wrapWidth(s string, width int) string {
	if width < 8 {
		width = 8
	}
	var b strings.Builder
	first := true
	for _, line := range strings.Split(s, "\n") {
		for {
			head, rest := cutCells(line, width)
			if !first {
				b.WriteByte('\n')
			}
			first = false
			b.WriteString(head)
			if rest == "" {
				break
			}
			line = rest
		}
	}
	return b.String()
}

func cutCells(s string, n int) (head, rest string) {
	if n <= 0 || s == "" {
		return s, ""
	}
	w := 0
	for i, r := range s {
		cw := 1
		if r == '\t' {
			cw = 4
		}
		if w+cw > n {
			if i == 0 {
				i = len(string(r))
			}
			return s[:i], s[i:]
		}
		w += cw
	}
	return s, ""
}

func (m *model) filterView() string {
	status := m.theme.okStyle().Render("all")
	if m.typed.Err != nil {
		status = m.theme.error().Render("invalid regex")
	} else if m.live.Pattern != "" {
		status = m.theme.okStyle().Render(fmt.Sprintf("%s matches", comma(m.matchCount())))
	} else if m.srcOnly != "" {
		status = m.theme.dim().Render("source " + shortSource(m.srcOnly))
	} else if m.nsOnly != "" {
		status = m.theme.dim().Render("ns " + m.nsOnly)
	}
	input := m.input.View()
	gap := m.width - lipgloss.Width(input) - lipgloss.Width(status)
	if gap < 1 {
		gap = 1
	}
	line := input + strings.Repeat(" ", gap) + status
	st := lipgloss.NewStyle()
	if m.focus == paneFilter {
		st = st.Foreground(m.theme.accent)
	}
	return st.Width(max(1, m.width)).MaxHeight(1).Render(line)
}

func (m *model) footerText() string {
	return " / filter   n ns   ←→ scroll   j/k move   f follow   p pause   d detail   s sources   e errors   ? help   q quit"
}

func (m *model) nsView() string {
	items := m.nsItems()
	counts := m.nsCounts()
	var b strings.Builder
	b.WriteString("namespace  (enter select   n/esc close)\n\n")
	for i, ns := range items {
		mark := "  "
		if i == m.nsSel {
			mark = "▸ "
		}
		dot := " "
		label := "*  all namespaces"
		n := 0
		if ns == "" {
			n = len(m.lines)
			if m.nsOnly == "" {
				dot = "●"
			}
		} else {
			label = ns
			n = counts[ns]
			if m.nsOnly == ns {
				dot = "●"
			}
		}
		b.WriteString(fmt.Sprintf("%s%s %-28s %6d\n", mark, dot, label, n))
	}
	if len(items) == 1 {
		b.WriteString("\n  (namespaces appear as logs arrive)\n")
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.accent).
		Padding(1, 2).
		Width(min(72, max(40, m.width-8))).
		MaxHeight(max(10, m.height-4)).
		Render(strings.TrimRight(b.String(), "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *model) errView() string {
	var b strings.Builder
	b.WriteString("errors  (e/esc close   c clear)\n\n")
	if len(m.errs) == 0 {
		b.WriteString("  (none)")
	} else {
		for i, e := range m.errs {
			mark := "  "
			if i == m.errSel {
				mark = "▸ "
			}
			n := ""
			if e.Count > 1 {
				n = fmt.Sprintf("  ×%d", e.Count)
			}
			b.WriteString(mark)
			b.WriteString(e.Time.Format("15:04:05"))
			b.WriteString("  ")
			b.WriteString(e.Text)
			b.WriteString(n)
			b.WriteByte('\n')
		}
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.err).
		Padding(1, 2).
		Width(min(100, max(40, m.width-6))).
		MaxHeight(max(8, m.height-4)).
		Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *model) helpView() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.accent).
		Padding(1, 2).
		Width(min(72, m.width-4)).
		Render(helpText)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func shortSource(id string) string {
	parts := strings.Split(id, "/")
	switch len(parts) {
	case 0:
		return id
	case 1:
		return parts[0]
	case 2:
		return parts[1]
	default:
		// pod/container, drop namespace
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
}

func truncatePlain(s string, w int) string {
	// lipgloss.Width handles ANSI; still cap runes roughly
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
