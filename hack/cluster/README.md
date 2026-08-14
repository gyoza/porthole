# Echo + Envoy Gateway test bed

Manifests for a single-node lab: `ealen/echo-server` behind Envoy Gateway, advertised by MetalLB.

```bash
kubectl apply -f hack/cluster/echo-gateway.yaml
```

Gateway VIP (MetalLB pool `172.16.0.240-249`):

```bash
kubectl get gateway -n echo
# ADDRESS 172.16.0.240
```

Hit it with a Host header (`echo.local` or `www.example.com`):

```bash
curl -H 'Host: echo.local' http://172.16.0.240/
curl -H 'Host: echo.local' http://172.16.0.240/api/v1/users
curl -X POST -H 'Host: echo.local' -d '{"user":"ada"}' http://172.16.0.240/login
curl -H 'Host: echo.local' 'http://172.16.0.240/boom?echo_code=500'
```

`echo_code` is how the echo-server returns 401/404/500 so Envoy writes mixed JSON access logs.

Tail those logs with porthole:

```bash
./bin/porthole -n envoy-gateway-system
# or
./bin/porthole -n echo
```

## Plain-text logs

```bash
kubectl apply -f hack/cluster/plain-logs.yaml
```

- `logs/chatter` — INFO/WARN/ERROR, klog, logfmt, fake combined lines (no JSON)
- `logs/nginx` — real nginx combined access + error logs

```bash
curl -H 'Host: nginx.local' http://172.16.0.240/
curl -H 'Host: nginx.local' http://172.16.0.240/nope   # 404 + nginx error line

# one stream — JSON and plain mixed, no format flag
./bin/porthole -A
```
