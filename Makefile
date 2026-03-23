.PHONY: build test test-hooks test-all

build:
	go build ./...

test:
	go test ./internal/...

test-hooks:
	go test ./hooks/...

test-all: test test-hooks
