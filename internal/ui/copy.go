package ui

import (
	"fmt"
	"os"
	"time"

	"github.com/atotto/clipboard"
	osc52 "github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

const osc52Max = 16 << 10 // big OSC 52 payloads can freeze the tty

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
		done := make(chan struct{})
		go func() {
			_ = clipboard.WriteAll(s)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(400 * time.Millisecond):
		}
		if len(s) <= osc52Max {
			seq := osc52.New(s)
			if os.Getenv("TMUX") != "" {
				seq = seq.Tmux()
			}
			_, _ = fmt.Fprint(os.Stderr, seq)
		}
		return copiedMsg{n: len(s)}
	}
}
