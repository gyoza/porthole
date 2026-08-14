package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gyoza/porthole/internal/parse"
	"github.com/gyoza/porthole/internal/source"
)

var palette = []string{
	"#7AD1FF", "#FF7A9A", "#B38CFF", "#7CFFB2", "#FFD37A",
	"#FF9A7A", "#7AFFE0", "#E07AFF", "#A0FF7A", "#7A9CFF",
}

type theme struct {
	fg       lipgloss.Color
	muted    lipgloss.Color
	accent   lipgloss.Color
	border   lipgloss.Color
	headerBg lipgloss.Color
	selBg    lipgloss.Color
	err      lipgloss.Color
	ok       lipgloss.Color
	warn     lipgloss.Color
	info     lipgloss.Color
	debug    lipgloss.Color
}

func defaultTheme() theme {
	return theme{
		fg:       lipgloss.Color("#E8EEF4"),
		muted:    lipgloss.Color("#8B9BB4"),
		accent:   lipgloss.Color("#98C1D9"),
		border:   lipgloss.Color("#3D5A80"),
		headerBg: lipgloss.Color("#1B2838"),
		selBg:    lipgloss.Color("#243447"),
		err:      lipgloss.Color("#EF476F"),
		ok:       lipgloss.Color("#80ED99"),
		warn:     lipgloss.Color("#FFD166"),
		info:     lipgloss.Color("#8ECAE6"),
		debug:    lipgloss.Color("#8D99AE"),
	}
}

func (t theme) header() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(t.accent).
		Background(t.headerBg).
		Bold(true).
		Padding(0, 1)
}

func (t theme) footer() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.muted)
}

func (t theme) borderBox(active bool) lipgloss.Style {
	c := t.border
	if active {
		c = t.accent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c)
}

func (t theme) title(active bool) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(t.muted).Padding(0, 1)
	if active {
		s = s.Foreground(t.accent).Bold(true)
	}
	return s
}

func (t theme) selected() lipgloss.Style {
	return lipgloss.NewStyle().Background(t.selBg).Foreground(t.fg)
}

func (t theme) dim() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.muted)
}

func (t theme) error() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.err)
}

func (t theme) okStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.ok)
}

func colorFor(seed string) lipgloss.Color {
	return lipgloss.Color(palette[source.HashColorIndex(seed, len(palette))])
}

func (t theme) statusColor(code int) lipgloss.Style {
	switch {
	case code >= 500:
		return lipgloss.NewStyle().Foreground(t.err).Bold(true)
	case code >= 400:
		return lipgloss.NewStyle().Foreground(t.warn).Bold(true)
	case code >= 300:
		return lipgloss.NewStyle().Foreground(t.info)
	case code >= 200:
		return lipgloss.NewStyle().Foreground(t.ok)
	default:
		return lipgloss.NewStyle().Foreground(t.fg)
	}
}

func (t theme) levelColor(level string) lipgloss.Style {
	switch strings.ToUpper(level) {
	case "ERROR", "ERR", "FATAL", "PANIC":
		return lipgloss.NewStyle().Foreground(t.err).Bold(true)
	case "WARN", "WARNING":
		return lipgloss.NewStyle().Foreground(t.warn).Bold(true)
	case "INFO":
		return lipgloss.NewStyle().Foreground(t.info)
	case "DEBUG", "TRACE":
		return lipgloss.NewStyle().Foreground(t.debug)
	default:
		return lipgloss.NewStyle().Foreground(t.fg)
	}
}

func (t theme) methodColor(m string) lipgloss.Style {
	switch strings.ToUpper(m) {
	case "GET":
		return lipgloss.NewStyle().Foreground(t.ok)
	case "POST":
		return lipgloss.NewStyle().Foreground(t.info)
	case "PUT", "PATCH":
		return lipgloss.NewStyle().Foreground(t.warn)
	case "DELETE":
		return lipgloss.NewStyle().Foreground(t.err)
	default:
		return lipgloss.NewStyle().Foreground(t.fg)
	}
}

func highlight(s string, re interface{ FindAllStringIndex(string, int) [][]int }, base, hi lipgloss.Style) string {
	if re == nil || s == "" {
		return base.Render(s)
	}
	idxs := re.FindAllStringIndex(s, -1)
	if len(idxs) == 0 {
		return base.Render(s)
	}
	var b strings.Builder
	last := 0
	for _, pair := range idxs {
		if pair[0] > last {
			b.WriteString(base.Render(s[last:pair[0]]))
		}
		b.WriteString(hi.Render(s[pair[0]:pair[1]]))
		last = pair[1]
	}
	if last < len(s) {
		b.WriteString(base.Render(s[last:]))
	}
	return b.String()
}

func paintHTTP(t theme, rec parse.Record, base lipgloss.Style) string {
	parts := make([]string, 0, 8)
	if !rec.Timestamp.IsZero() {
		parts = append(parts, t.dim().Render(rec.Timestamp.Format("15:04:05.000")))
	}
	if rec.Method != "" {
		parts = append(parts, t.methodColor(rec.Method).Render(pad(rec.Method, 6)))
	}
	if rec.Path != "" {
		parts = append(parts, base.Render(rec.Path))
	}
	if rec.Status > 0 {
		parts = append(parts, t.statusColor(rec.Status).Render(itoa(rec.Status)))
	}
	if rec.Duration != "" {
		parts = append(parts, t.dim().Render(rec.Duration))
	}
	if rec.Host != "" {
		parts = append(parts, t.dim().Render(rec.Host))
	}
	if rec.Flags != "" && rec.Flags != "-" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.warn).Render(rec.Flags))
	}
	return strings.Join(parts, "  ")
}

func paintApp(t theme, rec parse.Record, base lipgloss.Style) string {
	parts := make([]string, 0, 6)
	if !rec.Timestamp.IsZero() {
		parts = append(parts, t.dim().Render(rec.Timestamp.Format("15:04:05.000")))
	}
	if rec.Level != "" {
		parts = append(parts, t.levelColor(rec.Level).Render(pad(rec.Level, 5)))
	}
	if rec.Message != "" {
		parts = append(parts, base.Render(rec.Message))
	} else {
		parts = append(parts, base.Render(rec.Display))
	}
	return strings.Join(parts, "  ")
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= w {
		return s
	}
	if w <= 1 {
		return string(runes[:w])
	}
	return string(runes[:w-1]) + "…"
}
