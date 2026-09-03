BINARY  := vitals
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build build-all test race lint vet staticcheck ci run clean install coverage check-docs hooks-install

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

build-all:
	./build.sh $(VERSION)

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

staticcheck:
	@which staticcheck >/dev/null 2>&1 || go install honnef.co/go/tools/cmd/staticcheck@latest
	staticcheck ./...

lint: vet staticcheck

coverage:
	go test -coverprofile=coverage.out ./...
	python3 check_coverage.py coverage.out

check-docs:
	python3 check_docs.py

ci: lint race build-all check-docs

hooks-install:
	git config core.hooksPath .githooks
	@echo "pre-commit hook installed (runs gofmt/vet/staticcheck/race-test/coverage before every commit)"

run: build
	./$(BINARY) doctor

install: build
	install -m 0755 $(BINARY) $(or $(PREFIX),/usr/local)/bin/$(BINARY)

clean:
	rm -f $(BINARY)
	rm -rf dist
