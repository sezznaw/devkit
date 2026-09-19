BINARY   := devkit
MODULE   := github.com/sezznaw/devkit
REPO     ?= sezznaw/devkit
VERSION  ?= $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -s -w \
  -X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
  -X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
  -X $(MODULE)/internal/buildinfo.Date=$(DATE) \
  -X $(MODULE)/internal/buildinfo.Repo=$(REPO)

.PHONY: build install test lint clean snapshot

build:
	go build -ldflags '$(LDFLAGS)' -o bin/$(BINARY) .

install:
	go install -ldflags '$(LDFLAGS)' .

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -rf bin dist

# Local dry-run of the release pipeline (needs goreleaser installed).
snapshot:
	goreleaser release --snapshot --clean
