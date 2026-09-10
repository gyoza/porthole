package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/gyoza/porthole/internal/filter"
	"github.com/gyoza/porthole/internal/source"
	"github.com/gyoza/porthole/internal/ui"
	"k8s.io/client-go/kubernetes"
)

type flags struct {
	namespace  string
	allNS      bool
	selector   string
	container  string
	exclude    string
	include    []string
	excludeLog []string
	tail       int64
	since      time.Duration
	contexts   []string
	kubeconfig string
	demo       bool
	demoPods   int
	demoRate   int
	demoQuiet  int
	demoProg   int
	file       string
	stdin      bool
	timestamps bool
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
  porthole -i '6ef0ac35-0794-46fe-bec6-c6d89a420a29'
  porthole -i ERROR -e healthz
  porthole -s5m
  porthole -s1d
  porthole -l app=foo -c sidecar
  porthole --context prod1 --context prod2
  porthole --demo --demo-pods 80 --demo-rate 2000
  kubectl logs -f deploy/foo | porthole
`,
		Args:    cobra.MaximumNArgs(1),
		Version: versionString(),
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
	root.Flags().StringArrayVarP(&f.include, "include", "i", nil, "only show log lines matching this regex (repeatable)")
	root.Flags().StringArrayVarP(&f.excludeLog, "exclude", "e", nil, "hide log lines matching this regex (repeatable)")
	root.Flags().StringVar(&f.exclude, "exclude-container", "", "container name regex to skip")
	root.Flags().Int64Var(&f.tail, "tail", 200, "lines to start with from each container")
	root.Flags().VarP(newSinceValue(&f.since), "since", "s", "show logs newer than a relative duration (5s, 5m, 1h, 1d)")
	root.Flags().StringArrayVar(&f.contexts, "context", nil, "kubeconfig context (repeat once for a second cluster)")
	root.Flags().StringVar(&f.kubeconfig, "kubeconfig", "", "path to kubeconfig")
	root.Flags().BoolVar(&f.demo, "demo", false, "stream mixed fake logs (JSON + plain) without a cluster")
	root.Flags().IntVar(&f.demoPods, "demo-pods", 0, "unique pods for --demo (default 5)")
	root.Flags().IntVar(&f.demoRate, "demo-rate", 0, "lines per second for --demo (default 12)")
	root.Flags().IntVar(&f.demoQuiet, "demo-quiet", 0, "of those pods, emit rarely (stay in [context] after the ring wraps)")
	root.Flags().IntVar(&f.demoProg, "demo-progress", 0, "pods that emit curl/awscli \\r progress lines")
	root.Flags().StringVar(&f.file, "file", "", "read a log file instead of the cluster")
	root.Flags().BoolVar(&f.stdin, "stdin", false, "read log lines from stdin")
	root.Flags().BoolVarP(&f.timestamps, "timestamps", "t", false, "show parsed timestamps in [logs] (off by default; always on [json]/[raw])")

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
	inc, err := filter.Join(f.include)
	if err != nil {
		return fmt.Errorf("include: %w", err)
	}
	exc, err := filter.Join(f.excludeLog)
	if err != nil {
		return fmt.Errorf("exclude: %w", err)
	}
	opts := ui.Options{Query: query, Include: inc.Pattern, Exclude: exc.Pattern, ShowTime: f.timestamps}

	if f.demoPods > 0 || f.demoRate > 0 || f.demoQuiet > 0 || f.demoProg > 0 {
		f.demo = true
	}

	switch {
	case f.demo:
		dc := source.DemoConfig{Pods: f.demoPods, Rate: f.demoRate, Quiet: f.demoQuiet, Progress: f.demoProg}
		if dc.Pods == 0 {
			dc.Pods = 5
		}
		if dc.Rate == 0 {
			dc.Rate = 12
		}
		opts.Title = fmt.Sprintf("demo %d pods · %d/s", dc.Pods, dc.Rate)
		go func() {
			defer close(events)
			if err := source.DemoWith(ctx, dc, events); err != nil && ctx.Err() == nil {
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
		uiOpts, err := startKubeTails(ctx, f, query, events)
		if err != nil {
			return err
		}
		opts.Namespace = uiOpts.Namespace
		opts.Context = uiOpts.Context
		opts.Contexts = uiOpts.Contexts
		opts.Namespaces = uiOpts.Namespaces
		opts.SwitchNS = uiOpts.SwitchNS
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

func parseContexts(in []string) ([]string, error) {
	var out []string
	seen := map[string]struct{}{}
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			return nil, fmt.Errorf("duplicate --context %q", c)
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	if len(out) > 2 {
		return nil, fmt.Errorf("--context can be given at most twice (got %d)", len(out))
	}
	return out, nil
}

func startKubeTails(ctx context.Context, f flags, query string, events chan<- source.Event) (ui.Options, error) {
	names, err := parseContexts(f.contexts)
	if err != nil {
		return ui.Options{}, err
	}
	podRe, err := regexp.Compile(query)
	if err != nil {
		return ui.Options{}, fmt.Errorf("pod query: %w", err)
	}
	base := source.KubeOptions{
		Kubeconfig: f.kubeconfig,
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
			return ui.Options{}, fmt.Errorf("container regex: %w", err)
		}
		base.Container = re
	}
	if f.exclude != "" {
		re, err := regexp.Compile(f.exclude)
		if err != nil {
			return ui.Options{}, fmt.Errorf("exclude-container regex: %w", err)
		}
		base.Exclude = re
	}
	if len(names) == 0 {
		names = []string{""}
	}

	type built struct {
		name     string
		cs       kubernetes.Interface
		watchNS  string
		ns       string
		opts     source.KubeOptions
		switchCh chan string
	}
	var tails []built
	resolved := make([]string, 0, len(names))
	nsSeen := map[string]struct{}{}
	var nsList []string
	var watchNS0 string

	for _, cname := range names {
		opts := base
		opts.Context = cname
		cs, ns, err := source.BuildClient(opts)
		if err != nil {
			if cname != "" {
				return ui.Options{}, fmt.Errorf("context %s: %w", cname, err)
			}
			return ui.Options{}, err
		}
		got := source.CurrentContext(f.kubeconfig, cname)
		if got == "" {
			got = cname
		}
		if got == "" {
			got = "cluster"
		}
		opts.Context = got
		watchNS := ns
		if f.allNS {
			watchNS = ""
		}
		if watchNS0 == "" && watchNS != "" {
			watchNS0 = watchNS
		}
		for _, n := range source.ListNamespaces(ctx, cs) {
			if _, ok := nsSeen[n]; ok {
				continue
			}
			nsSeen[n] = struct{}{}
			nsList = append(nsList, n)
		}
		tails = append(tails, built{
			name:     got,
			cs:       cs,
			watchNS:  watchNS,
			ns:       ns,
			opts:     opts,
			switchCh: make(chan string, 1),
		})
		resolved = append(resolved, got)
	}

	if len(resolved) == 2 && resolved[0] == resolved[1] {
		return ui.Options{}, fmt.Errorf("duplicate --context %q", resolved[0])
	}

	uiOpts := ui.Options{
		Contexts:   resolved,
		Context:    strings.Join(resolved, ","),
		Namespaces: nsList,
	}
	if f.allNS {
		uiOpts.Namespace = "*"
	} else if f.namespace != "" {
		uiOpts.Namespace = f.namespace
	} else if watchNS0 != "" {
		uiOpts.Namespace = watchNS0
	}

	var wg sync.WaitGroup
	for i := range tails {
		wg.Add(1)
		t := tails[i]
		go func() {
			defer wg.Done()
			runOneTail(ctx, t.cs, t.watchNS, t.opts, t.switchCh, events)
		}()
	}
	go func() {
		wg.Wait()
		close(events)
	}()

	uiOpts.SwitchNS = func(target string) {
		for _, t := range tails {
			trySendNS(t.switchCh, target)
		}
	}
	return uiOpts, nil
}

func trySendNS(ch chan string, target string) {
	select {
	case ch <- target:
	default:
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- target:
		default:
		}
	}
}

func runOneTail(ctx context.Context, cs kubernetes.Interface, watchNS string, opts source.KubeOptions, switchCh <-chan string, events chan<- source.Event) {
	current := watchNS
	for {
		tctx, tcancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		tkopts := opts
		tkopts.Namespace = current
		tkopts.AllNS = current == ""
		ns := current
		go func() {
			done <- source.TailPods(tctx, cs, ns, tkopts, events)
		}()
		select {
		case <-ctx.Done():
			tcancel()
			<-done // TailPods may still be sending; closing `events` before this panics
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
				label := opts.Context
				if label == "" {
					label = "porthole"
				}
				events <- source.Event{
					Time:    time.Now(),
					Context: opts.Context,
					Pod:     label,
					Err:     "tail failed: " + err.Error(),
				}
			}
			select {
			case <-ctx.Done():
				return
			case next := <-switchCh:
				current = next
			case <-time.After(2 * time.Second):
			}
		}
	}
}
