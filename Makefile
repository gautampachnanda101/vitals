.DEFAULT_GOAL := help

BINARY  := vitals
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

BOLD  := \033[1m
CYAN  := \033[36m
RESET := \033[0m

.PHONY: help build build-all run install clean test race vet staticcheck lint coverage check-docs ci hooks-install

##@ Help

help: ## Show this help (default when you just run `make`)
	@printf "\n$(BOLD)vitals$(RESET) — make targets\n"
	@awk 'BEGIN {FS = ":.*##"}; \
		/^[a-zA-Z0-9_-]+:.*##/ { printf "  $(CYAN)%-16s$(RESET) %s\n", $$1, $$2 } \
		/^##@/ { printf "\n$(BOLD)%s$(RESET)\n", substr($$0, 5) }' $(MAKEFILE_LIST)
	@printf "\n"

##@ Build & run

build: ## Compile ./vitals for the current OS/arch
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

build-all: ## Cross-compile every release target into ./dist (see build.sh)
	./build.sh $(VERSION)

run: build ## Build, then run `vitals doctor`
	./$(BINARY) doctor

install: build ## Install to $PREFIX/bin (default /usr/local/bin)
	install -m 0755 $(BINARY) $(or $(PREFIX),/usr/local)/bin/$(BINARY)

clean: ## Remove build artifacts (./vitals, ./dist)
	rm -f $(BINARY)
	rm -rf dist

##@ Test & lint

test: ## Run the unit test suite
	go test ./...

race: ## Run the unit test suite with the race detector
	go test -race ./...

vet: ## go vet ./...
	go vet ./...

staticcheck: ## Run staticcheck (installs it first if missing)
	@which staticcheck >/dev/null 2>&1 || go install honnef.co/go/tools/cmd/staticcheck@latest
	staticcheck ./...

lint: vet staticcheck ## Run vet + staticcheck

##@ Coverage & docs

coverage: ## Run tests with coverage, enforce per-package floors (AGENTS.md's 95%+ rule)
	go test -coverprofile=coverage.out ./...
	python3 check_coverage.py coverage.out

check-docs: ## Verify docs/ links, roadmap nav, and breadcrumb navigation
	python3 check_docs.py

##@ Everything

ci: lint race build-all check-docs ## Run the full local CI-equivalent gate

hooks-install: ## Install the pre-commit hook (gofmt/vet/staticcheck/race/coverage/docs)
	git config core.hooksPath .githooks
	@echo "pre-commit hook installed (runs gofmt/vet/staticcheck/race-test/coverage/docs before every commit)"
