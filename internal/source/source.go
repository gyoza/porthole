// Package source produces a stream of log events from Kubernetes, files,
// stdin, or a built-in demo generator.
package source

import (
	"bufio"
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"strings"
	"time"
)

// Event is one log line from a named origin.
// If Err is set, this is a status/error notice, not a log line.
type Event struct {
	Time      time.Time
	Context   string
	Namespace string
	Pod       string
	Container string
	Line      string
	Err       string
}

// SourceID is the short label used in the TUI.
// Kubernetes lines are "ctx/ns/pod/container" so two clusters cannot collide.
func (e Event) SourceID() string {
	parts := make([]string, 0, 4)
	if e.Context != "" {
		parts = append(parts, e.Context)
	}
	if e.Namespace != "" {
		parts = append(parts, e.Namespace)
	}
	if e.Pod != "" {
		parts = append(parts, e.Pod)
	}
	if e.Container != "" {
		parts = append(parts, e.Container)
	}
	if len(parts) == 0 {
		return "stdin"
	}
	return strings.Join(parts, "/")
}

// ColorSeed is a stable key for assigning a pod color.
func (e Event) ColorSeed() string {
	if e.Pod != "" {
		switch {
		case e.Context != "" && e.Namespace != "":
			return e.Context + "/" + e.Namespace + "/" + e.Pod
		case e.Namespace != "":
			return e.Namespace + "/" + e.Pod
		default:
			return e.Pod
		}
	}
	return e.SourceID()
}

// HashColorIndex maps a seed onto a palette of n colors.
func HashColorIndex(seed string, n int) int {
	if n <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return int(h.Sum32() % uint32(n))
}

// ReaderConfig tails a file or stdin.
type ReaderConfig struct {
	Name      string
	Namespace string
	Pod       string
	Container string
}

// ReadLines scans r and sends one Event per line until EOF or cancel.
func ReadLines(ctx context.Context, r io.Reader, cfg ReaderConfig, out chan<- Event) error {
	sc := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		ev := Event{
			Time:      time.Now(),
			Namespace: cfg.Namespace,
			Pod:       cfg.Pod,
			Container: cfg.Container,
			Line:      sc.Text(),
		}
		if ev.Pod == "" {
			ev.Pod = cfg.Name
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case out <- ev:
		}
	}
	return sc.Err()
}

// OpenFile opens a log file for ReadLines.
func OpenFile(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return f, nil
}
