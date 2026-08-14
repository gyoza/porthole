BINARY := porthole
PKG := ./cmd/porthole

.PHONY: build test tidy run-demo run-file fmt vet

build:
	go build -o bin/$(BINARY) $(PKG)

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
