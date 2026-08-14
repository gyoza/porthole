# porthole

A Stern-style Kubernetes log tailer with a paneled terminal UI.

Follow matching pods, filter with a live regex, and read JSON or ordinary
text in the same stream. There is no format flag — each line is sniffed
on its own.

<img width="890" height="557" alt="porthole" src="https://github.com/user-attachments/assets/9c0f034e-f4bb-4a69-a4ac-a89830f9fc21" />

## Install

Needs Go 1.22+. Cluster tailing uses the Kubernetes API directly (client-go)
and only needs a working kubeconfig `~/.kube/config`. 

Switches `--demo` and `--file` work with no cluster at all.

```bash
go install github.com/gyoza/porthole/cmd/porthole@latest
```

From a clone:

```bash
git clone https://github.com/gyoza/porthole.git
cd porthole
make build
./bin/porthole
```

## Usage

Same shape as Stern. The first argument is a pod-name regex.

```bash
porthole                              # current namespace, every pod
porthole -n envoy-gateway-system
porthole -A                           # all namespaces
porthole -A 'envoy.*|nginx|chatter'
porthole -l app=foo -c sidecar
porthole --since 10m --tail 500
```

Without a cluster:

```bash
porthole --demo                       # mixed fake JSON + plain logs
porthole --file testdata/mixed.log
kubectl logs -f deploy/foo | porthole
```

`--demo` and `--file` are sources, not format switches.

### Flags

| Flag | Meaning |
|------|---------|
| `-n`, `--namespace` | Kubernetes namespace (defaults to the current context) |
| `-A`, `--all-namespaces` | Follow pods in every namespace |
| `-l`, `--selector` | Label selector |
| `-c`, `--container` | Container name regex |
| `--exclude-container` | Container name regex to skip |
| `--tail` | Lines to start with from each container (default 200) |
| `--since` | Only logs newer than a duration (`5m`, `1h`) |
| `--context` | kubeconfig context |
| `--kubeconfig` | Path to kubeconfig |
| `--demo` | Generated mixed logs, no cluster |
| `--file` | Read a file instead of the cluster |
| `--stdin` | Read stdin (also used automatically when piped) |

### Keys

| Key | Action |
|-----|--------|
| `/` | Focus the live regex |
| `enter` / `esc` | Leave the filter |
| `n` | Namespace picker (always available) |
| `j` `k` / arrows | Move the selected line |
| `g` / `G` | Top / bottom |
| `f` | Follow the tail |
| `p` | Pause ingest |
| `d` | Toggle selected-line detail |
| `s` | Toggle the source list |
| `tab` | Cycle panes |
| `enter` on a source | Pin the stream to that pod |
| `e` | Open the error list |
| `?` | Help |
| `q` | Quit |

`n` lists `*` (all namespaces) plus cluster namespaces and any that have
appeared in the stream. Enter selects; the view filters and the tailer
retargets. Header shows `ns=logs` or `ns=*`.

The regex is compiled as you type. A half-typed pattern keeps the last
valid filter so the stream does not go blank.

Client-go messages (throttling, watch drops) stay in a top-right badge
(`2 errors  e`). They do not print under the TUI.

## What it understands

Each line is classified independently.

**JSON** — a line that is `{...}` or `text prefix {...}`. Nested JSON in
`message` / `msg` is unwrapped one level (common with collectors).

| Role | Keys |
|------|------|
| Time | `start_time`, `ts`, `time`, `timestamp`, `@timestamp` |
| Level | `level`, `severity`, `lvl` |
| Message | `msg`, `message`, `error` |
| HTTP | `method`, `x-envoy-origin-path` / `path`, `response_code` / `status`, `duration`, `:authority` / `host` |

Envoy Gateway's default access log becomes:

```text
21:00:01.123  GET   /get  200  12ms  www.example.com
```

**Plain text** — nginx combined and error logs, `TS LEVEL msg`, `[ERROR] …`,
klog (`I0814 …]`), and logfmt (`level=error msg="…"`). Anything else stays
as the raw line.

The detail pane pretty-prints JSON when the line is JSON, and shows the
raw line otherwise.

## Lab cluster

Manifests under `hack/cluster/` stand up Envoy Gateway, an echo server,
nginx, and a chatter that writes ordinary text logs:

```bash
kubectl apply -f hack/cluster/echo-gateway.yaml
kubectl apply -f hack/cluster/plain-logs.yaml

curl -H 'Host: echo.local' http://$GATEWAY_IP/
curl -H 'Host: nginx.local' http://$GATEWAY_IP/

./bin/porthole -A
```

See `hack/cluster/README.md`.

## Development

```bash
make test
make build
make run-demo
make run-file
```

## License

MIT
