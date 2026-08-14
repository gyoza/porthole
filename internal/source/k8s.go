package source

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubeOptions control how pods are selected and tailed.
type KubeOptions struct {
	Kubeconfig string
	Context    string
	Namespace  string
	AllNS      bool
	Selector   string
	PodQuery   *regexp.Regexp
	Container  *regexp.Regexp
	Exclude    *regexp.Regexp
	TailLines  int64
	Since      time.Duration
	Timestamps bool
}

// BuildClient loads a kubeconfig (or in-cluster config) and returns a client
// plus the resolved namespace when the user did not pass one.
func BuildClient(opts KubeOptions) (*kubernetes.Clientset, string, error) {
	loading := clientcmd.NewDefaultClientConfigLoadingRules()
	if opts.Kubeconfig != "" {
		loading.ExplicitPath = opts.Kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if opts.Context != "" {
		overrides.CurrentContext = opts.Context
	}
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, overrides)

	restCfg, err := cfg.ClientConfig()
	if err != nil {
		// fall back to in-cluster
		restCfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, "", fmt.Errorf("kubeconfig: %w", err)
		}
	}
	// rest.Config.Timeout is http.Client.Timeout — it applies to the entire
	// request, including Watch and log Follow streams. A 15s cap here is what
	// produced "tail failed: pod watch closed" after the first burst of logs.
	// Leave it at zero so follow/watch can stay open; list still uses a
	// per-call context deadline below.
	restCfg.Timeout = 0
	// Following many pods at once (porthole -A) exceeds client-go's default
	// 5 QPS / 10 burst and prints "client-side throttling" to the terminal,
	// which wrecks the TUI. Give the tailer enough headroom.
	if restCfg.QPS < 50 {
		restCfg.QPS = 50
	}
	if restCfg.Burst < 100 {
		restCfg.Burst = 100
	}

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, "", fmt.Errorf("kubernetes client: %w", err)
	}

	ns := opts.Namespace
	if ns == "" && !opts.AllNS {
		ns, _, err = cfg.Namespace()
		if err != nil || ns == "" {
			ns = "default"
		}
	}
	return cs, ns, nil
}

// CurrentContext returns the kubeconfig current-context name.
func CurrentContext(kubeconfig, override string) string {
	if override != "" {
		return override
	}
	loading := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loading.ExplicitPath = kubeconfig
	}
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, &clientcmd.ConfigOverrides{})
	raw, err := cfg.RawConfig()
	if err != nil {
		return ""
	}
	return raw.CurrentContext
}

// ListNamespaces returns cluster namespace names, sorted.
func ListNamespaces(ctx context.Context, cs kubernetes.Interface) []string {
	list, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(list.Items))
	for i := range list.Items {
		if n := list.Items[i].Name; n != "" {
			out = append(out, n)
		}
	}
	return out
}

// TailPods watches matching pods and follows container logs.
// It only returns when ctx is cancelled, or the first list/watch cannot start.
// A dropped watch is reconnected instead of aborting the tailer.
func TailPods(ctx context.Context, cs kubernetes.Interface, ns string, opts KubeOptions, out chan<- Event) error {
	if opts.PodQuery == nil {
		opts.PodQuery = regexp.MustCompile(".*")
	}
	if opts.TailLines <= 0 {
		opts.TailLines = 200
	}

	var mu sync.Mutex
	streams := map[string]context.CancelFunc{}
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, cancel := range streams {
			cancel()
		}
	}()

	syncPod := func(pod *corev1.Pod) {
		if !opts.PodQuery.MatchString(pod.Name) {
			return
		}
		for _, csStatus := range pod.Status.ContainerStatuses {
			if !containerWanted(csStatus.Name, opts) {
				continue
			}
			if csStatus.State.Running == nil && csStatus.State.Terminated == nil {
				continue
			}
			key := fmt.Sprintf("%s/%s/%s#%d", pod.Namespace, pod.Name, csStatus.Name, csStatus.RestartCount)
			mu.Lock()
			if _, ok := streams[key]; ok {
				mu.Unlock()
				continue
			}
			sctx, cancel := context.WithCancel(ctx)
			streams[key] = cancel
			mu.Unlock()
			go followContainer(sctx, cs, pod.Namespace, pod.Name, csStatus.Name, opts, out)
		}
	}

	dropPod := func(pod *corev1.Pod) {
		prefix := pod.Namespace + "/" + pod.Name + "/"
		mu.Lock()
		defer mu.Unlock()
		for key, cancel := range streams {
			if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
				cancel()
				delete(streams, key)
			}
		}
	}

	listOpts := metav1.ListOptions{LabelSelector: opts.Selector}
	first := true
	backoff := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		lctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		pods, err := cs.CoreV1().Pods(ns).List(lctx, listOpts)
		cancel()
		if err != nil {
			if first {
				return fmt.Errorf("list pods: %w", err)
			}
			emitErr(ctx, out, "porthole", "list pods", err)
			if !sleepCtx(ctx, backoff) {
				return ctx.Err()
			}
			backoff = nextBackoff(backoff)
			continue
		}
		first = false
		backoff = time.Second

		for i := range pods.Items {
			p := pods.Items[i]
			syncPod(&p)
		}

		wopts := listOpts
		wopts.ResourceVersion = pods.ResourceVersion
		wopts.AllowWatchBookmarks = true
		w, err := cs.CoreV1().Pods(ns).Watch(ctx, wopts)
		if err != nil {
			emitErr(ctx, out, "porthole", "watch pods", err)
			if !sleepCtx(ctx, backoff) {
				return ctx.Err()
			}
			backoff = nextBackoff(backoff)
			continue
		}

		alive := consumeWatch(ctx, w, syncPod, dropPod)
		w.Stop()
		if !alive {
			return ctx.Err()
		}
		emitErr(ctx, out, "porthole", "watch", fmt.Errorf("pod watch closed; reconnecting"))
		if !sleepCtx(ctx, backoff) {
			return ctx.Err()
		}
		backoff = nextBackoff(backoff)
	}
}

func consumeWatch(ctx context.Context, w watch.Interface, syncPod, dropPod func(*corev1.Pod)) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case ev, ok := <-w.ResultChan():
			if !ok {
				return true
			}
			if ev.Type == watch.Error || ev.Type == watch.Bookmark {
				continue
			}
			pod, ok := ev.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			switch ev.Type {
			case watch.Added, watch.Modified:
				syncPod(pod)
			case watch.Deleted:
				dropPod(pod)
			}
		}
	}
}

func containerWanted(name string, opts KubeOptions) bool {
	if opts.Exclude != nil && opts.Exclude.MatchString(name) {
		return false
	}
	if opts.Container != nil && !opts.Container.MatchString(name) {
		return false
	}
	return true
}

func followContainer(ctx context.Context, cs kubernetes.Interface, ns, pod, container string, opts KubeOptions, out chan<- Event) {
	tail := opts.TailLines
	since := opts.Since
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		logOpts := &corev1.PodLogOptions{
			Container:  container,
			Follow:     true,
			Timestamps: opts.Timestamps,
		}
		if tail > 0 {
			t := tail
			logOpts.TailLines = &t
		}
		if since > 0 {
			sec := int64(since.Seconds())
			if sec < 1 {
				sec = 1
			}
			logOpts.SinceSeconds = &sec
		}

		opened, err := streamLogs(ctx, cs, ns, pod, container, logOpts, out)
		if ctx.Err() != nil {
			return
		}
		if opened {
			// Reconnect should not replay the last --tail window.
			tail = 0
			since = 0
			backoff = time.Second
		}
		if err != nil {
			emitErr(ctx, out, ns+"/"+pod+"/"+container, "stream logs", err)
		}
		if !sleepCtx(ctx, backoff) {
			return
		}
		if !opened {
			backoff = nextBackoff(backoff)
		}
	}
}

func streamLogs(ctx context.Context, cs kubernetes.Interface, ns, pod, container string, logOpts *corev1.PodLogOptions, out chan<- Event) (bool, error) {
	stream, err := cs.CoreV1().Pods(ns).GetLogs(pod, logOpts).Stream(ctx)
	if err != nil {
		return false, err
	}
	defer stream.Close()

	sc := bufio.NewScanner(stream)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		ev := Event{
			Time:      time.Now(),
			Namespace: ns,
			Pod:       pod,
			Container: container,
			Line:      sc.Text(),
		}
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case out <- ev:
		}
	}
	return true, sc.Err()
}

func emitErr(ctx context.Context, out chan<- Event, src, msg string, err error) {
	if err == nil || ctx.Err() != nil {
		return
	}
	ev := Event{
		Time: time.Now(),
		Pod:  src,
		Err:  msg + ": " + err.Error(),
	}
	if parts := splitSrc(src); len(parts) == 3 {
		ev.Namespace, ev.Pod, ev.Container = parts[0], parts[1], parts[2]
	}
	select {
	case <-ctx.Done():
	case out <- ev:
	}
}

func splitSrc(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > 15*time.Second {
		return 15 * time.Second
	}
	return d
}
