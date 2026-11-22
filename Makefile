VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test vet
build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/moipctl ./cmd/moipctl
test:
	go test ./...
vet:
	go vet ./...
