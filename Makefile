.PHONY: build test test-hooks test-all

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags "-X main.version=$(VERSION)" ./...

test:
	go test ./internal/...

test-hooks:
	go test ./hooks/...

test-all: test test-hooks
