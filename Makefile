BIN     := bin/faultline
PKG     := ./cmd/faultline
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# golangci-lint is installed into GOPATH/bin by `make tools`.
GOBIN   := $(shell go env GOPATH)/bin

.PHONY: build test lint tools run clean

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) $(PKG)

test:
	go test -race ./...

lint:
	go vet ./...
	$(GOBIN)/golangci-lint run

# Pinned to the last release that builds with the go.mod Go version (1.25);
# v2.13.x requires Go 1.26 and CI runs with GOTOOLCHAIN=local.
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.0

run: build
	./$(BIN) serve

clean:
	rm -rf bin
