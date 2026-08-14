BINARY := porthole
PKG := ./cmd/porthole
VERSION ?= 0.0.1
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test tidy run-demo run-file fmt vet dist

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_$(VERSION)_linux_amd64 $(PKG)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)_$(VERSION)_darwin_amd64 $(PKG)
	cd dist && sha256sum $(BINARY)_$(VERSION)_linux_amd64 $(BINARY)_$(VERSION)_darwin_amd64 > SHA256SUMS

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

run-file: build
	./bin/$(BINARY) --file testdata/mixed.log
