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

const version = "0.1.0"

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
		Short: "Windowed Kubernetes log tailer with live regex and JSON intelligence",
		Long: `porthole is a Stern-like log tailer with a paneled terminal UI.

It follows matching pods, parses JSON automatically (Envoy Gateway access
logs, zap/slog/logrus, and JSON-with-a-text-prefix), and filters the stream
with a regex that recompiles as you type.

Examples:
  porthole                              # all pods in the current namespace
  porthole -n envoy-gateway-system      # a namespace
  porthole -l gateway.envoyproxy.io/owning-gateway-name=eg
  porthole --demo                       # generated Envoy-style JSON
  porthole --file ./app.jsonl
  kubectl logs -f deploy/foo | porthole --stdin
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
	root.Flags().BoolVar(&f.demo, "demo", false, "stream generated Envoy Gateway-style JSON logs")
	root.Flags().StringVar(&f.file, "file", "", "read a log file instead of the cluster")
	root.Flags().BoolVar(&f.stdin, "stdin", false, "read log lines from stdin")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(f flags, query string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

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
		watchNS := ns
		if f.allNS {
			watchNS = ""
			opts.Namespace = "*"
		}
		go func() {
			defer close(events)
			if err := source.TailPods(ctx, cs, watchNS, kopts, events); err != nil && ctx.Err() == nil {
				// surfaced via a synthetic event so the TUI can show it
				events <- source.Event{
					Time: time.Now(),
					Pod:  "porthole",
					Line: fmt.Sprintf(`{"level":"error","msg":"tail failed","error":%q}`, err.Error()),
				}
			}
		}()
	}

	prog := tea.NewProgram(
		ui.New(opts),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	go pump(ctx, events, prog)

	if _, err := prog.Run(); err != nil {
		return err
	}
	cancel()
	return nil
}

func pump(ctx context.Context, events <-chan source.Event, prog *tea.Program) {
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
			batch = append(batch, ev)
			if len(batch) >= 64 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
