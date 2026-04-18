SHELL := /bin/bash
GO ?= go
PKG := ./...
LINT_TIMEOUT ?= 3m

.PHONY: all check release build dev clean fmt vet lint tidy test cover e2e vulncheck

all: check

fmt:
	@out=$$($(GO) fmt $(PKG)); if [ -n "$$out" ]; then echo "gofmt changed:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet $(PKG)

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --timeout=$(LINT_TIMEOUT); \
	else \
		echo "[WARN] golangci-lint not installed, skipping"; \
	fi

tidy:
	@cp -f go.mod go.mod.bak 2>/dev/null; [ -f go.sum ] && cp -f go.sum go.sum.bak || true
	@$(GO) mod tidy
	@mod_changed=0; sum_changed=0; \
		if [ -f go.mod.bak ] && ! diff -q go.mod.bak go.mod >/dev/null 2>&1; then mod_changed=1; fi; \
		if [ -f go.sum.bak ] && ! diff -q go.sum.bak go.sum >/dev/null 2>&1; then sum_changed=1; fi; \
		rm -f go.mod.bak go.sum.bak; \
		if [ $$mod_changed -eq 1 ] || [ $$sum_changed -eq 1 ]; then \
			echo "go.mod/go.sum not tidy; run 'go mod tidy' and commit"; exit 1; \
		fi

test:
	$(GO) test -race $(PKG)

cover:
	$(GO) test -coverprofile=cover.out $(PKG)
	$(GO) tool cover -func=cover.out | tail -30

e2e:
	@if [ -n "$$(find ./e2e -name '*.go' 2>/dev/null)" ]; then \
		$(GO) test -tags=e2e -race ./e2e/...; \
	else \
		echo "[SKIP] no e2e tests yet; integration tests live in internal/*_test.go"; \
	fi

vulncheck:
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck $(PKG); \
	else \
		echo "[WARN] govulncheck not installed, skipping"; \
	fi

# Version string injected into the binary at build time. Prefers git tag;
# falls back to short sha + dirty marker; ultimately "dev" outside a git repo.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o blog-server ./cmd/server
	@./blog-server -version | sed 's/^/[OK] built /'

dev:
	$(GO) run ./cmd/server

check: fmt vet lint tidy test vulncheck
	@echo "[OK] make check all green"

release: check e2e build
	@sha256sum blog-server > blog-server.sha256
	@echo "[OK] make release complete — ./blog-server + ./blog-server.sha256 ready"
	@echo "    next: start the binary then 'make smoke URL=http://...' for runtime gates"

# smoke runs the runtime-only gates (lighthouse resource checks, header baseline,
# migrate-test). Caller must start the server first; pass URL=... to target.
URL ?= http://127.0.0.1:8080
smoke:
	./scripts/check-headers.sh $(URL)
	./scripts/lighthouse.sh $(URL)
	./scripts/migrate-test.sh .
	@echo "[OK] smoke PASS"

clean:
	rm -f blog-server blog-server.sha256 cover.out
