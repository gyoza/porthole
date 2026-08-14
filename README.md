# porthole

Stern-style multi-pod follow, in a paneled terminal. There is no json/plain
flag — each line is sniffed on its own.

```
┌ porthole  ctx=Default  ns=*  5 src  42 / 1,204  ·  LIVE ─┐
│ sources           │ logs                                 │
│ ● envoy-eg-7f8c   │ 21:01:02  GET  /get   200  12ms      │
│   nginx           │ 21:01:03  INFO worker tick n=18      │
│   chatter         │ 10.1.0.4  GET  /index.html  200      │
│                   ├ detail ──────────────────────────────┤
│                   │ { "method": "GET", ... }             │
├───────────────────┴──────────────────────────────────────┤
│ / 401|5[0-9]{2}                              2 matches   │
└──────────────────────────────────────────────────────────┘
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

JSON (Envoy Gateway, zap, slog, prefixed `{...}`) and ordinary text (nginx
combined, nginx error, `TS LEVEL msg`, klog, logfmt) share the same stream.

### Keys

| Key | Action |
|-----|--------|
| `/` | Focus the live regex |
| `enter` / `esc` | Leave the filter |
| `j` `k` / arrows | Move the selected line |
| `g` / `G` | Top / bottom |
| `f` | Follow the tail |
| `p` | Pause ingest |
| `d` | Toggle selected-line detail |
| `s` | Toggle the source list |
| `tab` | Cycle panes |
| `enter` on a source | Pin the stream to that pod |
| `?` | Help |
| `q` | Quit |

## Install

```bash
go install github.com/gyoza/porthole/cmd/porthole@latest
```

Or from a clone: `make build` then `./bin/porthole`.

Needs Go 1.22+ and a kubeconfig. `--demo` and `--file` are only for running
without a cluster; they are not format switches.

## License

MIT
