package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gyoza/porthole/internal/parse"
)

func (m *model) View() string {
	if m.width == 0 {
		return "starting porthole…"
	}
	if m.showHelp {
		return m.helpView()
	}

	header := m.theme.header().Width(m.width).Render(truncate(m.headerText(), m.width-2))
	body := m.bodyView()
	filter := m.filterView()
	footer := m.theme.footer().Width(m.width).Render(truncate(m.footerText(), m.width))

	return lipgloss.JoinVertical(lipgloss.Left, header, body, filter, footer)
}

func (m *model) bodyView() string {
	srcW, logH, _, detailH := m.geom()
	logs := m.logsView(logH)
	if m.showDetail && detailH > 0 {
		logs = lipgloss.JoinVertical(lipgloss.Left, logs, m.detailView())
	}
	if srcW > 0 {
		return lipgloss.JoinHorizontal(lipgloss.Top, m.sourcesView(srcW), logs)
	}
	return logs
}

func (m *model) logsView(innerH int) string {
	active := m.focus == paneLogs
	w := m.width
	if srcW, _, _, _ := m.geom(); srcW > 0 {
		w = m.width - srcW
	}
	title := m.theme.title(active).Render("logs")
	rows := make([]string, 0, innerH)
	if len(m.filtered) == 0 {
		msg := "waiting for logs…"
		if len(m.lines) > 0 {
			msg = "no lines match the current regex"
		}
		if m.err != nil {
			msg = m.theme.error().Render(m.err.Error())
		}
		rows = append(rows, m.theme.dim().Render("  "+msg))
	} else {
		end := m.offset + innerH
		if end > len(m.filtered) {
			end = len(m.filtered)
		}
		for i := m.offset; i < end; i++ {
			ln := m.lines[m.filtered[i]]
			rows = append(rows, m.renderLine(ln, w-4, i == m.cursor))
		}
	}
	for len(rows) < innerH {
		rows = append(rows, "")
	}
	content := strings.Join(rows, "\n")
	box := m.theme.borderBox(active).Width(max(0, w-2)).Render(title + "\n" + content)
	return box
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
	if m.live.Regexp != nil && ln.Rec.Kind == parse.KindPlain {
		hi := lipgloss.NewStyle().Foreground(lipgloss.Color("#1B2838")).Background(m.theme.warn)
		body = highlight(ln.Rec.Display, m.live.Regexp, base, hi)
	}

	line := prefix + " " + body
	if selected {
		line = m.theme.selected().Render("▸ ") + line
	} else {
		line = "  " + line
	}
	return truncatePlain(line, width)
}

func (m *model) sourcesView(w int) string {
	active := m.focus == paneSources
	innerH := m.height - 5
	if m.showDetail {
		innerH = m.height - 5
	}
	if innerH < 3 {
		innerH = 3
	}
	title := m.theme.title(active).Render("sources")
	rows := make([]string, 0, len(m.sources))
	for i, s := range m.sources {
		mark := " "
		if s.ID == m.srcOnly {
			mark = "●"
		}
		label := fmt.Sprintf("%s %-18s %5d", mark, truncate(shortSource(s.ID), 18), s.Count)
		st := lipgloss.NewStyle().Foreground(s.Color)
		if i == m.srcSel && active {
			st = st.Background(m.theme.selBg).Bold(true)
		}
		rows = append(rows, st.Render(label))
	}
	if len(rows) == 0 {
		rows = append(rows, m.theme.dim().Render("  (none yet)"))
	}
	// keep a window around selection
	content := strings.Join(rows, "\n")
	return m.theme.borderBox(active).Width(w - 2).Height(max(3, innerH)).Render(title + "\n" + content)
}

func (m *model) detailView() string {
	active := m.focus == paneDetail
	title := "json"
	if ln, ok := m.selected(); ok {
		if ln.Rec.JSONBytes == nil {
			title = "raw"
		} else {
			title = "json · " + shortSource(ln.Source)
		}
	}
	head := m.theme.title(active).Render(title)
	body := m.detail.View()
	w := m.width
	if srcW, _, _, _ := m.geom(); srcW > 0 {
		w = m.width - srcW
	}
	return m.theme.borderBox(active).Width(max(0, w-2)).Render(head + "\n" + body)
}

func (m *model) filterView() string {
	status := m.theme.okStyle().Render("all")
	if m.typed.Err != nil {
		status = m.theme.error().Render("invalid regex — keeping last valid")
	} else if m.live.Pattern != "" {
		status = m.theme.okStyle().Render(fmt.Sprintf("%s matches", comma(m.matchCount())))
	} else if m.srcOnly != "" {
		status = m.theme.dim().Render("source " + shortSource(m.srcOnly))
	}
	input := m.input.View()
	gap := m.width - lipgloss.Width(input) - lipgloss.Width(status) - 2
	if gap < 1 {
		gap = 1
	}
	line := input + strings.Repeat(" ", gap) + status
	if m.focus == paneFilter {
		return lipgloss.NewStyle().Foreground(m.theme.accent).Render(line)
	}
	return line
}

func (m *model) footerText() string {
	if m.err != nil {
		return "error: " + m.err.Error()
	}
	return " / filter   j/k move   f follow   p pause   d detail   s sources   ? help   q quit"
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
