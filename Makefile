.PHONY: proto build test

proto:
	cd proto && buf generate
	cp github.com/fyrash/fyra-cli/proto/gen/*.go proto/gen/
	rm -rf github.com/

BINARY_NAME ?= fyra

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags "-X main.binaryName=$(BINARY_NAME) -X main.version=$(VERSION)" -o bin/$(BINARY_NAME) ./cmd/client

test:
	CGO_ENABLED=1 go test -race ./...
