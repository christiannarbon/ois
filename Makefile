VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := github.com/christiannarbon/ois/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.PHONY: build test lint repo check integration clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/ois ./cmd/ois

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)
	go vet ./...

repo:
	./scripts/check-repo.sh

check: lint test repo

integration: build
	go test -tags integration -count=1 -timeout 30m ./test/integration/...

clean:
	rm -rf bin
