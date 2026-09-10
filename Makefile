BINARY := porthole
PKG := ./cmd/porthole
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test tidy run-demo run-load run-file fmt vet dist load-cluster unload-cluster

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_$(VERSION)_linux_amd64 $(PKG)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_$(VERSION)_darwin_amd64 $(PKG)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_$(VERSION)_darwin_arm64 $(PKG)
	cd dist && sha256sum $(BINARY)_$(VERSION)_linux_amd64 $(BINARY)_$(VERSION)_darwin_amd64 $(BINARY)_$(VERSION)_darwin_arm64 > SHA256SUMS

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	gofmt -w ./cmd ./internal

vet:
	go vet ./...

run-demo: build
	./bin/$(BINARY) --demo

# In-process: 80 pods, 2k lines/s, quiet + \r progress. No cluster.
run-load: build
	./bin/$(BINARY) --demo --demo-pods 80 --demo-rate 2000 --demo-quiet 10 --demo-progress 2

run-file: build
	./bin/$(BINARY) --file testdata/mixed.log

# Real pods on the current kube context. Scale noisy if you want more pain.
load-cluster:
	kubectl apply -f hack/cluster/noise.yaml
	@echo "wait: kubectl -n noise get pods"
	@echo "tail: ./bin/porthole -n noise"
	@echo "more: kubectl -n noise scale deploy/noisy --replicas=40"

unload-cluster:
	kubectl delete ns noise --ignore-not-found
