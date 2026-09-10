BIN_DIR := bin
BINARY := $(BIN_DIR)/nmt
VERSION ?= $(shell cat VERSION 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)
PREFIX ?= /usr/local
MANDIR ?= $(PREFIX)/share/man/man1

.PHONY: all build test test-race vet fmt fmt-check lint check coverage run install man clean

all: build

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed on:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

lint: vet
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "staticcheck not installed; skipping"; \
	fi

check: fmt-check vet test

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

run:
	go run . $(ARGS)

man:
	install -Dm644 docs/nmt.1 $(DESTDIR)$(MANDIR)/nmt.1

install: build man
	install -Dm755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/nmt

clean:
	rm -rf $(BIN_DIR) coverage.out
