# porthole

A windowed Kubernetes log tailer — Stern's multi-pod follow, a paneled terminal UI, a regex that recompiles as you type, and automatic JSON intelligence for things like Envoy Gateway.

```
┌ porthole  ctx=Default  ns=envoy-gateway-system  3 src  42 / 1,204  ·  LIVE ─┐
│ sources              │ logs                                                │
│ ● envoy-eg-7f8c9d    │ 21:01:02.441  GET   /get        200  12ms  example  │
│   envoy-eg-2a1b4c    │ 21:01:03.012  POST  /login      401   4ms  auth     │
│   envoy-gateway-0    │ 21:01:03.880  INFO  reconciling gateway             │
│                      ├ json ───────────────────────────────────────────────┤
│                      │ { "method": "GET", "response_code": 200, ... }      │
├──────────────────────┴─────────────────────────────────────────────────────┤
│ / 401|5[0-9]{2}                                              2 matches     │
│  / filter   j/k move   f follow   p pause   d detail   ? help   q quit     │
└────────────────────────────────────────────────────────────────────────────┘
```

## Why

Stern is great at multiplexing pod logs. The moment those logs are JSON — Envoy Gateway's default access log, a zap/slog controller, a collector that prefixes a timestamp — you end up piping through `jq` and losing the live, interactive feel.

porthole keeps the stream on screen:

- **Windowed TUI** — sources, compact log lines, and a pretty JSON detail pane
- **Live regex** — compiled on every keystroke; a half-typed pattern keeps the last valid filter
- **JSON, automatically** — Envoy Gateway access logs, zap/slog/logrus, and `prefix {json}` lines
- **Same sources as Stern** — pod-name regex, namespace, label selector, container regex
- **Works without a cluster** — `--demo`, `--file`, or pipe stdin

## Install

```bash
go install github.com/gyoza/porthole/cmd/porthole@latest
```

Or from a clone:

```bash
make build
./bin/porthole --demo
```

Needs Go 1.22+ and, for cluster tailing, a working kubeconfig (`kubectl` is enough).

## Usage

```bash
porthole                              # current namespace, every pod
porthole -n envoy-gateway-system      # one namespace
porthole -A 'envoy.*'                 # all namespaces, name regex
porthole -l app=foo -c sidecar
porthole --since 10m --tail 500

porthole --demo                       # generated Envoy-style JSON
porthole --file ./testdata/mixed.log
kubectl logs -f deploy/foo | porthole --stdin
```

### Keys

| Key | Action |
|-----|--------|
| `/` | Focus the live regex |
| `enter` / `esc` | Leave the filter |
| `j` `k` / arrows | Move the selected line |
| `g` / `G` | Top / bottom |
| `f` | Follow the tail |
| `p` | Pause ingest |
| `d` | Toggle JSON detail |
| `s` | Toggle the source list |
| `tab` | Cycle panes |
| `enter` on a source | Pin the stream to that pod |
| `?` | Help |
| `q` | Quit |

### JSON it understands

Detected when a line is `{...}` or `text prefix {...}`. Nested JSON in `message`/`msg` is unwrapped one level (common with collectors).

| Role | Keys |
|------|------|
| Time | `start_time`, `ts`, `time`, `timestamp`, `@timestamp` |
| Level | `level`, `severity`, `lvl` |
| Message | `msg`, `message`, `error` |
| HTTP | `method`, `x-envoy-origin-path` / `path`, `response_code` / `status`, `duration`, `:authority` / `host` |

Envoy Gateway's default access log is treated as an HTTP line:

```text
21:00:01.123  GET   /get  200  12ms  www.example.com
```

Everything else with a level/message becomes an app line. The original object is always in the detail pane.

## Development

```bash
make test
make run-demo
make run-file
```

## License

MIT
