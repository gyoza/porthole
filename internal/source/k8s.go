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
	// Don't sit forever on a dead API server.
	if restCfg.Timeout == 0 {
		restCfg.Timeout = 15 * time.Second
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

// TailPods watches matching pods and follows container logs.
func TailPods(ctx context.Context, cs kubernetes.Interface, ns string, opts KubeOptions, out chan<- Event) error {
	if opts.PodQuery == nil {
		opts.PodQuery = regexp.MustCompile(".*")
	}
	if opts.TailLines <= 0 {
		opts.TailLines = 200
	}

	listOpts := metav1.ListOptions{LabelSelector: opts.Selector}
	pods, err := cs.CoreV1().Pods(ns).List(ctx, listOpts)
	if err != nil {
		return fmt.Errorf("list pods: %w", err)
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

	for i := range pods.Items {
		p := pods.Items[i]
		syncPod(&p)
	}

	listOpts.ResourceVersion = pods.ResourceVersion
	w, err := cs.CoreV1().Pods(ns).Watch(ctx, listOpts)
	if err != nil {
		return fmt.Errorf("watch pods: %w", err)
	}
	defer w.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-w.ResultChan():
			if !ok {
				return fmt.Errorf("pod watch closed")
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
	logOpts := &corev1.PodLogOptions{
		Container:  container,
		Follow:     true,
		Timestamps: opts.Timestamps,
		TailLines:  &opts.TailLines,
	}
	if opts.Since > 0 {
		sec := int64(opts.Since.Seconds())
		if sec < 1 {
			sec = 1
		}
		logOpts.SinceSeconds = &sec
	}

	req := cs.CoreV1().Pods(ns).GetLogs(pod, logOpts)
	stream, err := req.Stream(ctx)
	if err != nil {
		select {
		case <-ctx.Done():
		case out <- Event{
			Time:      time.Now(),
			Namespace: ns,
			Pod:       pod,
			Container: container,
			Line:      fmt.Sprintf(`{"level":"error","msg":"failed to stream logs","error":%q}`, err.Error()),
		}:
		}
		return
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
			return
		case out <- ev:
		}
	}
}
