VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
NPM ?= npm

.PHONY: all build frontend test vet ui-check
# The binary embeds internal/ui/dist: run `make frontend` first to have the web UI in it.
all: frontend build
build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/moipctl ./cmd/moipctl
# Build the web UI (needs Node.js) and put it where the binary embeds it from. .gitkeep stays.
frontend:
	cd frontend && $(NPM) ci && $(NPM) run build
	find internal/ui/dist -mindepth 1 ! -name .gitkeep -delete
	cp -r frontend/dist/. internal/ui/dist/
test:
	go test ./...
	cd frontend && $(NPM) test && $(NPM) run typecheck
vet:
	go vet ./...
# Try the web UI against fake nodes in headless Chrome (needs `make all` first, python3, node and google-chrome).
ui-check:
	test/ui/run.sh
