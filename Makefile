# aimgen Makefile
# Stdlib-only toolchain: build, test, and lint via the Go toolchain plus gofmt.

BINARY      := aimgen
PKG         := ./...
GOFLAGS     ?=
GO          ?= go
INSTALL_DIR ?= $(shell $(GO) env GOBIN)
ifeq ($(INSTALL_DIR),)
INSTALL_DIR := $(shell $(GO) env GOPATH)/bin
endif

# Version metadata, derived from git and injected at build time. A dirty working
# tree gets a "-dirty" suffix automatically; an untagged tree falls back to the
# dev version. Override VERSION on the command line or in CI as needed.
GIT_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.9.0-dev)
VERSION     ?= $(patsubst v%,%,$(GIT_VERSION))
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Inject version/commit/date into the binary.
LDFLAGS ?= -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

# Platforms built by `make dist` (os/arch).
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.DEFAULT_GOAL := help

## help: Show this help.
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed -E 's/^## //' | awk -F': ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

## build: Compile the aimgen binary into ./bin.
.PHONY: build
build:
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BINARY) .

## test: Run all tests.
.PHONY: test
test:
	$(GO) test $(GOFLAGS) $(PKG)

## cover: Run tests with a coverage report.
.PHONY: cover
cover:
	$(GO) test $(GOFLAGS) -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -func=coverage.out | tail -n 1

## cover-html: Open an HTML coverage report.
.PHONY: cover-html
cover-html: cover
	$(GO) tool cover -html=coverage.out

## vet: Run go vet.
.PHONY: vet
vet:
	$(GO) vet $(PKG)

## fmt: Format all Go source in place.
.PHONY: fmt
fmt:
	$(GO) fmt $(PKG)

## tidy: Tidy go.mod/go.sum.
.PHONY: tidy
tidy:
	$(GO) mod tidy

## lint: Check formatting and run go vet (CI-friendly, non-mutating).
.PHONY: lint
lint:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needs to run on:"; echo "$$unformatted"; exit 1; \
	fi
	$(GO) vet $(PKG)

## run: Build and run (pass args via ARGS="...").
.PHONY: run
run:
	$(GO) run . $(ARGS)

## install: Install the binary to GOBIN/GOPATH bin.
.PHONY: install
install:
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' .
	@echo "Installed $(BINARY) to $(INSTALL_DIR)"

## init-config: Write a sample config to the default location.
.PHONY: init-config
init-config:
	$(GO) run . --init-config

## clean: Remove build artifacts.
.PHONY: clean
clean:
	rm -rf bin coverage.out dist

## dist: Cross-compile release archives + checksums into ./dist.
.PHONY: dist
dist:
	@rm -rf dist && mkdir -p dist
	@echo "Building $(BINARY) $(VERSION) (commit $(COMMIT))"
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
		name="$(BINARY)_$(VERSION)_$${os}_$${arch}"; \
		stage="dist/$$name"; \
		echo "  -> $$os/$$arch"; \
		mkdir -p "$$stage"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath \
			-ldflags '$(LDFLAGS) -s -w' -o "$$stage/$(BINARY)$$ext" . || exit 1; \
		cp README.md LICENSE "$$stage/"; \
		if [ "$$os" = "windows" ]; then \
			( cd dist && zip -qr "$$name.zip" "$$name" ); \
		else \
			tar -czf "dist/$$name.tar.gz" -C dist "$$name"; \
		fi; \
		rm -rf "$$stage"; \
	done
	@echo "Writing checksums.txt"
	@( cd dist && \
		if command -v sha256sum >/dev/null 2>&1; then \
			sha256sum *.tar.gz *.zip 2>/dev/null > checksums.txt; \
		else \
			shasum -a 256 *.tar.gz *.zip 2>/dev/null > checksums.txt; \
		fi )
	@echo "Artifacts in ./dist:" && ls -1 dist

## check: Run the full local gate (lint + test + build).
.PHONY: check
check: lint test build
