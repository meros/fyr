# Fyr. CGO is off: the SQLite driver is pure Go and NixOS has no global gcc.
export CGO_ENABLED := 0
OUT ?= bin
BUILD_ID ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null)$(shell git diff --quiet 2>/dev/null || echo -dirty-$$(date +%s))

.PHONY: build web web-deps node-lts go test test-go test-web test-timing install run

build: web go

# The web client builds and tests on a Node LTS only, on every OS: .nvmrc
# says lts/* for fnm and nvm, and the flake's dev shell has nixpkgs' nodejs.
node-lts:
	@node -e 'process.release.lts || (console.error("Node " + process.version + " is not an LTS release: run `fnm use` or `nvm use` (.nvmrc)"), process.exit(1))'

web: node-lts
	cd web && npm ci --no-audit --no-fund --silent && npm run build

go:
	mkdir -p $(OUT)
	go build -ldflags "-X main.buildID=$(BUILD_ID)" -o $(OUT)/fyrd ./cmd/fyrd
	go build -o $(OUT)/fyr ./cmd/fyr

test: test-go test-web

test-go:
	go vet ./...
	go test ./...

# A fresh checkout (CI) has no web/node_modules; install them from the lock file once.
web-deps: node-lts
	cd web && { [ -d node_modules ] || npm ci --no-audit --no-fund --silent; }

# The layout test's timing budgets (V0.38, V0.32) measure wall time and fail on a busy machine (#50), so the deploy
# gate skips them by default. CI runs `make test FYR_LAYOUT_TIMING=1`; `make test-timing` runs them on their own.
FYR_LAYOUT_TIMING ?= 0

test-web: web-deps
	cd web && npm run typecheck && npm test && npm run build && FYR_LAYOUT_TIMING=$(FYR_LAYOUT_TIMING) npm run test:layout

test-timing: web-deps
	cd web && npm run build && FYR_LAYOUT_TIMING=1 npm run test:layout

# First install: put the build under ~/.local/share/fyr; then run `bin/fyr`.
install: build
	$(OUT)/fyr install $(OUT)

# Development without the launcher: serves the client from web/dist on disk.
run: go
	$(OUT)/fyrd serve -web web/dist
