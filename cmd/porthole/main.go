package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/gyoza/porthole/internal/source"
	"github.com/gyoza/porthole/internal/ui"
)

var version = "0.0.1"

type flags struct {
	namespace  string
	allNS      bool
	selector   string
	container  string
	exclude    string
	tail       int64
	since      time.Duration
	context    string
	kubeconfig string
	demo       bool
	file       string
	stdin      bool
}

func main() {
	var f flags

	root := &cobra.Command{
		Use:   "porthole [pod-query]",
		Short: "Windowed multi-pod log tailer (like Stern)",
		Long: `porthole tails matching pods the way Stern does, in a paneled TUI.

There is no json/plain switch. Each line is sniffed on its own: Envoy
Gateway JSON, zap/slog, nginx combined, klog, or leftover text.

The filter box is a live regex, recompiled as you type.

Examples:
  porthole                              # current namespace, every pod
  porthole -n envoy-gateway-system
  porthole -A 'envoy.*'
  porthole -l app=foo -c sidecar
  kubectl logs -f deploy/foo | porthole
`,
		Args:    cobra.MaximumNArgs(1),
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ".*"
			if len(args) == 0 {
				// keep default
			} else {
				query = args[0]
			}
			return run(f, query)
		},
	}

	root.Flags().StringVarP(&f.namespace, "namespace", "n", "", "Kubernetes namespace (defaults to current context)")
	root.Flags().BoolVarP(&f.allNS, "all-namespaces", "A", false, "follow pods in all namespaces")
	root.Flags().StringVarP(&f.selector, "selector", "l", "", "label selector")
	root.Flags().StringVarP(&f.container, "container", "c", "", "container name regex")
	root.Flags().StringVar(&f.exclude, "exclude-container", "", "container name regex to skip")
	root.Flags().Int64Var(&f.tail, "tail", 200, "lines to start with from each container")
	root.Flags().DurationVar(&f.since, "since", 0, "show logs newer than a relative duration (e.g. 5m)")
	root.Flags().StringVar(&f.context, "context", "", "kubeconfig context")
	root.Flags().StringVar(&f.kubeconfig, "kubeconfig", "", "path to kubeconfig")
	root.Flags().BoolVar(&f.demo, "demo", false, "stream mixed fake logs (JSON + plain) without a cluster")
	root.Flags().StringVar(&f.file, "file", "", "read a log file instead of the cluster")
	root.Flags().BoolVar(&f.stdin, "stdin", false, "read log lines from stdin")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(f flags, query string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	klogCh := make(chan string, 64)
	source.CaptureKlog(func(line string) {
		select {
		case klogCh <- line:
		default:
		}
	})

	events := make(chan source.Event, 8192)
	opts := ui.Options{Query: query}

	switch {
	case f.demo:
		opts.Title = "demo"
		go func() {
			defer close(events)
			if err := source.Demo(ctx, events); err != nil && ctx.Err() == nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}()
	case f.file != "":
		opts.Title = "file:" + f.file
		fh, err := source.OpenFile(f.file)
		if err != nil {
			return err
		}
		go func() {
			defer close(events)
			defer fh.Close()
			_ = source.ReadLines(ctx, fh, source.ReaderConfig{Name: f.file, Pod: f.file}, events)
		}()
	case f.stdin || !term.IsTerminal(int(os.Stdin.Fd())):
		opts.Title = "stdin"
		go func() {
			defer close(events)
			_ = source.ReadLines(ctx, os.Stdin, source.ReaderConfig{Name: "stdin", Pod: "stdin"}, events)
		}()
	default:
		podRe, err := regexp.Compile(query)
		if err != nil {
			return fmt.Errorf("pod query: %w", err)
		}
		kopts := source.KubeOptions{
			Kubeconfig: f.kubeconfig,
			Context:    f.context,
			Namespace:  f.namespace,
			AllNS:      f.allNS,
			Selector:   f.selector,
			PodQuery:   podRe,
			TailLines:  f.tail,
			Since:      f.since,
		}
		if f.container != "" {
			re, err := regexp.Compile(f.container)
			if err != nil {
				return fmt.Errorf("container regex: %w", err)
			}
			kopts.Container = re
		}
		if f.exclude != "" {
			re, err := regexp.Compile(f.exclude)
			if err != nil {
				return fmt.Errorf("exclude-container regex: %w", err)
			}
			kopts.Exclude = re
		}
		cs, ns, err := source.BuildClient(kopts)
		if err != nil {
			return err
		}
		opts.Namespace = ns
		opts.Context = source.CurrentContext(f.kubeconfig, f.context)
		opts.Namespaces = source.ListNamespaces(ctx, cs)
		watchNS := ns
		if f.allNS {
			watchNS = ""
			opts.Namespace = "*"
		}
		switchCh := make(chan string, 1)
		opts.SwitchNS = func(target string) {
			select {
			case switchCh <- target:
			default:
				select {
				case <-switchCh:
				default:
				}
				select {
				case switchCh <- target:
				default:
				}
			}
		}
		go func() {
			defer close(events)
			current := watchNS
			for {
				tctx, tcancel := context.WithCancel(ctx)
				done := make(chan error, 1)
				tkopts := kopts
				tkopts.Namespace = current
				tkopts.AllNS = current == ""
				go func() {
					done <- source.TailPods(tctx, cs, current, tkopts, events)
				}()
				select {
				case <-ctx.Done():
					tcancel()
					return
				case next := <-switchCh:
					tcancel()
					<-done
					current = next
				case err := <-done:
					tcancel()
					if ctx.Err() != nil {
						return
					}
					if err != nil {
						events <- source.Event{
							Time: time.Now(),
							Pod:  "porthole",
							Err:  "tail failed: " + err.Error(),
						}
					}
					select {
					case <-ctx.Done():
						return
					case next := <-switchCh:
						current = next
					case <-time.After(2 * time.Second):
						// retry same target
					}
				}
			}
		}()
	}

	prog := tea.NewProgram(
		ui.New(opts),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	go pump(ctx, events, klogCh, prog)

	if _, err := prog.Run(); err != nil {
		return err
	}
	cancel()
	return nil
}

func pump(ctx context.Context, events <-chan source.Event, klogCh <-chan string, prog *tea.Program) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var batch []source.Event
	flush := func() {
		if len(batch) == 0 {
			return
		}
		prog.Send(ui.LogBatchMsg(batch))
		batch = nil
	}
	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case ev, ok := <-events:
			if !ok {
				flush()
				prog.Send(ui.DoneMsg{})
				return
			}
			if ev.Err != "" {
				prog.Send(ui.StatusErrMsg{Time: ev.Time, Text: ev.Err})
				continue
			}
			batch = append(batch, ev)
			if len(batch) >= 64 {
				flush()
			}
		case line := <-klogCh:
			prog.Send(ui.StatusErrMsg{Time: time.Now(), Text: line})
		case <-ticker.C:
			flush()
		}
	}
}
