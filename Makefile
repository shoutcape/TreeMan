BINARY     := treeman
MODULE     := github.com/shoutcape/treeman
CMD        := ./cmd/treeman

VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE       := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS    := -X main.version=$(VERSION) \
              -X main.commit=$(COMMIT) \
              -X main.date=$(DATE)

.PHONY: build test lint clean install tidy

## build: compile the binary to ./bin/treeman
build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

## test: run all tests
test:
	go test ./...

## lint: run go vet (add golangci-lint later)
lint:
	go vet ./...

## tidy: tidy and verify go.mod / go.sum
tidy:
	go mod tidy
	go mod verify

## install: install binary to GOPATH/bin
install:
	go install -ldflags "$(LDFLAGS)" $(CMD)

## clean: remove build artifacts
clean:
	rm -rf bin/

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
