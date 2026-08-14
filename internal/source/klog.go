package source

import (
	"bytes"
	"flag"
	"sync"

	"k8s.io/klog/v2"
)

// CaptureKlog stops client-go / klog writing to the terminal (those lines
// sit under the TUI and break the pane layout) and delivers each line to hook.
func CaptureKlog(hook func(string)) {
	klogSink.set(hook)

	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	fs.SetOutput(&nopWriter{})
	klog.InitFlags(fs)
	_ = fs.Set("logtostderr", "false")
	_ = fs.Set("alsologtostderr", "false")
	_ = fs.Set("stderrthreshold", "FATAL")
	klog.SetOutput(&klogWriter{})
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

type klogHook struct {
	mu   sync.Mutex
	hook func(string)
}

func (h *klogHook) set(fn func(string)) {
	h.mu.Lock()
	h.hook = fn
	h.mu.Unlock()
}

func (h *klogHook) emit(s string) {
	h.mu.Lock()
	fn := h.hook
	h.mu.Unlock()
	if fn != nil && s != "" {
		fn(s)
	}
}

var klogSink klogHook

type klogWriter struct{}

func (klogWriter) Write(p []byte) (int, error) {
	for _, line := range bytes.Split(p, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		klogSink.emit(cleanKlog(string(line)))
	}
	return len(p), nil
}

func cleanKlog(s string) string {
	// "I0814 22:20:56.241213  42104 request.go:700] Waited for …"
	if i := bytes.IndexByte([]byte(s), ']'); i >= 0 && i+1 < len(s) {
		rest := bytes.TrimSpace([]byte(s[i+1:]))
		if len(rest) > 0 {
			return string(rest)
		}
	}
	return s
}
