# Fyr. CGO is off: the SQLite driver is pure Go and NixOS has no global gcc.
export CGO_ENABLED := 0
OUT ?= bin
BUILD_ID ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null)$(shell git diff --quiet 2>/dev/null || echo -dirty-$$(date +%s))

.PHONY: build web go test test-go test-web install run

build: web go

web:
	cd web && npm ci --no-audit --no-fund --silent && npm run build

go:
	mkdir -p $(OUT)
	go build -ldflags "-X main.buildID=$(BUILD_ID)" -o $(OUT)/fyrd ./cmd/fyrd
	go build -o $(OUT)/fyr ./cmd/fyr

test: test-go test-web

test-go:
	go vet ./...
	go test ./...

test-web:
	cd web && npm run typecheck && npm test && npm run build && npm run test:layout

# First install: put the build under ~/.local/share/fyr; then run `bin/fyr`.
install: build
	$(OUT)/fyr install $(OUT)

# Development without the launcher: serves the client from web/dist on disk.
run: go
	$(OUT)/fyrd serve -web web/dist
