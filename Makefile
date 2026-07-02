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

# Inject version/commit/date into the binary if those vars are defined later.
LDFLAGS ?=

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
	rm -rf bin coverage.out

## check: Run the full local gate (lint + test + build).
.PHONY: check
check: lint test build
