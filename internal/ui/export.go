package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gyoza/porthole/internal/parse"
)

type exportedMsg struct {
	path string
	n    int
	err  error
}

func (m *model) exportSanitized() string {
	var b strings.Builder
	for _, idx := range m.filtered {
		if idx < 0 || idx >= len(m.lines) {
			continue
		}
		ln := m.lines[idx]
		b.WriteString(m.sanitizedLine(ln))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *model) exportRaw() string {
	var b strings.Builder
	for _, ln := range m.lines {
		if ln.Source != "" {
			b.WriteString(ln.Source)
			b.WriteByte('\t')
		}
		raw := ln.Ev.Line
		if raw == "" {
			raw = ln.Rec.Raw
		}
		b.WriteString(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

func (m *model) sanitizedLine(ln logLine) string {
	src := shortSource(ln.Source)
	prefix := fmt.Sprintf("%-22s", truncate(src, 22))
	if ctx := m.lineContext(ln); ctx != "" {
		prefix = fmt.Sprintf("%-8s %-16s", truncate(ctx, 8), truncate(src, 16))
	}
	body := ln.Rec.Display
	if !m.showTime {
		body = parse.StripLeadingTime(body)
	}
	return prefix + " " + body
}

func writeExport(kind, body string) tea.Cmd {
	return func() tea.Msg {
		name := fmt.Sprintf("porthole-%s-%s.log", kind, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			return exportedMsg{path: name, err: err}
		}
		n := 0
		if body != "" {
			n = strings.Count(body, "\n")
		}
		return exportedMsg{path: name, n: n}
	}
}
