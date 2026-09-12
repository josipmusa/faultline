BIN     := bin/faultline
PKG     := ./cmd/faultline
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

UI_DIR  := web
UI_OUT  := $(UI_DIR)/out
TS_DIR  := clients/ts

# The ui build tag embeds web/out into the binary. A checkout with no Node
# toolchain still builds: without the tag the binary says the UI is missing
# rather than failing to compile. `make ui` is what produces the export.
UI_TAG  := $(if $(wildcard $(UI_OUT)/index.html),-tags ui,)

# golangci-lint is installed into GOPATH/bin by `make tools`.
GOBIN   := $(shell go env GOPATH)/bin
GOLANGCI_VERSION := v2.13.2

.PHONY: build ui clients test test-go test-ui test-clients lint lint-go lint-ui lint-clients tools run schema clean

build:
	go build $(UI_TAG) -ldflags "-X main.version=$(VERSION)" -o $(BIN) $(PKG)

# Builds the static export the binary embeds. Run it before `make build`, or
# after changing anything under web/.
ui:
	cd $(UI_DIR) && npm ci && npm run build

# Installs the TypeScript client's toolchain. Its tests drive a real Faultline,
# so they need a binary; `make test-clients` builds one.
clients:
	cd $(TS_DIR) && npm ci

test: test-go test-ui test-clients

test-go:
	go test -race ./...
# The tag changes what internal/admin serves at /, so that package is the one
# worth a second pass; nothing else behaves differently under it.
	$(if $(UI_TAG),go test -race $(UI_TAG) ./internal/admin/,@true)

lint: lint-go lint-ui lint-clients

lint-go:
	go vet ./...
	$(GOBIN)/golangci-lint run

# The UI halves skip rather than fail when the toolchain is absent, so `make
# test` and `make lint` stay usable in a checkout that has never run `make ui`.
test-ui:
	@$(call in-dir,$(UI_DIR),npm test,UI tests,make ui)

lint-ui:
	@$(call in-ui,npm run lint,UI lint)

# The client's tests start the binary they were given, so the build comes
# first: what they exercise is the Faultline this checkout produces.
test-clients: build
	@$(call in-dir,$(TS_DIR),npm test,client tests,make clients)

lint-clients:
	@$(call in-dir,$(TS_DIR),npm run lint,client lint,make clients)

define in-dir
if [ -d $(1)/node_modules ]; then cd $(1) && $(2); \
else echo "skipping $(3): $(1)/node_modules is missing, run $(4)"; fi
endef

define in-ui
$(call in-dir,$(UI_DIR),$(1),$(2),make ui)
endef

# Installs the prebuilt binary rather than building from source, so the linter
# version stays independent of the Go toolchain. `go install` would need a
# toolchain matching golangci-lint's own go directive, which CI cannot fetch
# because setup-go sets GOTOOLCHAIN=local. The install script verifies the
# release checksum.
tools:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_VERSION)/install.sh \
		| sh -s -- -b $(GOBIN) $(GOLANGCI_VERSION)

# schema/faultline.schema.json is generated from the fault catalogue, so a new
# fault reaches editors without anyone writing it out twice. A test fails when
# the checked in copy is stale.
schema:
	go test ./internal/config -run TestPublishedSchemaIsUpToDate -update

run: build
	./$(BIN) serve

clean:
	rm -rf bin
