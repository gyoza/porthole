package ui

import (
	"fmt"
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

func (m *model) pane(name, extra string, active bool, w, h int, body string) string {
	if w < 2 || h < 2 {
		return ""
	}
	innerW, innerH := w-2, h-2
	c := m.theme.border
	if active {
		c = m.theme.accent
	}
	br := lipgloss.NewStyle().Foreground(c)

	topInner := "─[" + name + "]"
	if extra != "" {
		ex := truncate(" "+extra, max(0, innerW-lipgloss.Width(topInner)))
		topInner += ex
	}
	if lipgloss.Width(topInner) > innerW {
		topInner = truncate(topInner, innerW)
	}
	fill := innerW - lipgloss.Width(topInner)
	if fill < 0 {
		fill = 0
	}
	top := br.Render("╭" + topInner + strings.Repeat("─", fill) + "╮")

	lines := strings.Split(clipLines(body, innerH), "\n")
	var b strings.Builder
	b.WriteString(top)
	b.WriteByte('\n')
	for _, line := range lines {
		line = parse.Sanitize(line)
		pad := innerW - lipgloss.Width(line)
		if pad < 0 {
			line = truncatePlain(line, innerW)
			pad = innerW - lipgloss.Width(line)
		}
		if pad < 0 {
			pad = 0
		}
		b.WriteString(br.Render("│"))
		b.WriteString(line)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(br.Render("│"))
		b.WriteByte('\n')
	}
	b.WriteString(br.Render("╰" + strings.Repeat("─", innerW) + "╯"))
	return b.String()
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
		left := m.sourcesView(ly, 0)
		if ly.srcH2 > 0 {
			left = lipgloss.JoinVertical(lipgloss.Left, left, m.sourcesView(ly, 1))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
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
	extra := ""
	if m.logCol > 0 {
		extra = fmt.Sprintf("←%d", m.logCol)
	}
	return m.pane("logs", extra, m.focus == paneLogs, ly.logW, ly.logH, strings.Join(rows, "\n"))
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
	label := fmt.Sprintf("%-22s", truncate(src, 22))
	if m.dualContext() {
		ctx := sourceContext(ln.Source)
		label = fmt.Sprintf("%-8s %-16s", truncate(ctx, 8), truncate(src, 16))
	}
	prefix := srcSt.Render(label)

	re := m.live.Regexp
	var body string
	switch ln.Rec.Kind {
	case parse.KindHTTP:
		body = paintHTTP(m.theme, ln.Rec, base, re, m.theme.warn, m.showTime)
	case parse.KindApp:
		body = paintApp(m.theme, ln.Rec, base, re, m.theme.warn, m.showTime)
	default:
		if re != nil && re.MatchString(ln.Rec.Display) {
			hi := base.Background(m.theme.warn)
			body = highlight(ln.Rec.Display, re, base, hi)
		} else {
			body = base.Render(ln.Rec.Display)
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

func (m *model) sourcesView(ly frame, which int) string {
	focus := paneSources
	h, avail, sel := ly.srcH, ly.srcRows, m.srcSel
	name := "sources"
	list := m.sources
	if m.dualContext() && which < len(m.opts.Contexts) {
		ctx := m.opts.Contexts[which]
		name = "sources · " + ctx
		list = m.sourcesFor(ctx)
		if which == 1 {
			focus = paneSources2
			h, avail, sel = ly.srcH2, ly.srcRows2, m.srcSel2
		}
	}
	rows := make([]string, 0, len(list))
	for i, s := range list {
		mark := " "
		if s.ID == m.srcOnly {
			mark = "●"
		}
		label := fmt.Sprintf("%s %-16s %5d", mark, truncate(shortSource(s.ID), 16), s.Count)
		st := lipgloss.NewStyle().Foreground(s.Color)
		if s.Count == 0 {
			st = m.theme.dim()
		}
		if i == sel && m.focus == focus {
			st = st.Background(m.theme.selBg).Bold(true)
		}
		rows = append(rows, st.Render(truncate(label, max(4, ly.srcW-4))))
	}
	if len(rows) == 0 {
		rows = append(rows, m.theme.dim().Render("  (none yet)"))
	}
	if start := srcWindow(sel, len(rows), avail); start > 0 {
		rows = rows[start:]
	}
	return m.pane(name, "", m.focus == focus, ly.srcW, h, strings.Join(rows, "\n"))
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
	name, extra := "raw", ""
	if ln, ok := m.selected(); ok {
		if len(ln.Rec.JSONBytes) > 0 {
			name = "json"
		}
		extra = shortSource(ln.Source)
		if !ln.Rec.Timestamp.IsZero() {
			extra = ln.Rec.Timestamp.Format("15:04:05.000") + "  " + extra
		}
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
	return m.pane(name, extra, m.focus == paneDetail, ly.detW, ly.detH, strings.Join(body, "\n"))
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
	return " / filter   n ns   ←→ scroll   j/k move   f follow   t time   p pause   d detail   s sources   e errors   ? help   q quit"
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

func sourceContext(id string) string {
	i := strings.IndexByte(id, '/')
	if i <= 0 {
		return ""
	}
	return id[:i]
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
		// pod/container, drop context and namespace
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
