BIN     := bin/faultline
PKG     := ./cmd/faultline
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# golangci-lint is installed into GOPATH/bin by `make tools`.
GOBIN   := $(shell go env GOPATH)/bin
GOLANGCI_VERSION := v2.13.2

.PHONY: build test lint tools run clean

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) $(PKG)

test:
	go test -race ./...

lint:
	go vet ./...
	$(GOBIN)/golangci-lint run

# Installs the prebuilt binary rather than building from source, so the linter
# version stays independent of the Go toolchain. `go install` would need a
# toolchain matching golangci-lint's own go directive, which CI cannot fetch
# because setup-go sets GOTOOLCHAIN=local. The install script verifies the
# release checksum.
tools:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_VERSION)/install.sh \
		| sh -s -- -b $(GOBIN) $(GOLANGCI_VERSION)

run: build
	./$(BIN) serve

clean:
	rm -rf bin
