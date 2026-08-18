package ui

import (
	"fmt"
	"os"

	"github.com/atotto/clipboard"
	osc52 "github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

type copiedMsg struct{ n int }

func (m *model) selectedCopy() (string, bool) {
	ln, ok := m.selected()
	if !ok {
		return "", false
	}
	return ln.Rec.Pretty(), true
}

func copyToClipboard(s string) tea.Cmd {
	return func() tea.Msg {
		_ = clipboard.WriteAll(s)
		seq := osc52.New(s)
		if os.Getenv("TMUX") != "" {
			seq = seq.Tmux()
		}
		_, _ = fmt.Fprint(os.Stderr, seq)
		return copiedMsg{n: len(s)}
	}
}
