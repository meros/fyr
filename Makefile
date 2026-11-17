# Fyr. CGO is off: the SQLite driver is pure Go and NixOS has no global gcc.
export CGO_ENABLED := 0
# #167: these name the live install (its runner and ready file). A make run from an agent's shell never passes them on.
unexport FYR_RUNNER FYR_LAUNCHER FYR_READY_FILE FYR_LAUNCHER_PID
OUT ?= bin
BUILD_ID ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null)$(shell git diff --quiet 2>/dev/null || echo -dirty-$$(date +%s))

.PHONY: build web web-deps node-lts go test test-go test-web test-timing install run go-vet go-test web-typecheck web-unit web-layout web-auth

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

# The phases run side by side (-j): the build of web/dist first, because the Go packages embed it and the layout test
# serves it; then go vet, go test, the typecheck, the web unit tests and the layout test at once. Output stays grouped
# per phase. `make test-go` and `make test-web` run one side, in series.
# GNU make before 4.0 (macOS's /usr/bin/make is 3.81) has no --output-sync; its output then interleaves.
SYNC := $(if $(filter 3.%,$(MAKE_VERSION)),,--output-sync=target)

test: web-deps
	cd web && npm run build
	$(MAKE) -j5 $(SYNC) go-vet go-test web-typecheck web-unit web-layout
	$(MAKE) web-auth # alone: beside the layout test its Chrome and Go build break the long-task budget

test-go: go-vet go-test

go-vet:
	go vet ./...

go-test:
	go test ./...

# A fresh checkout (CI) has no web/node_modules; install them from the lock file once.
web-deps: node-lts
	cd web && { [ -d node_modules ] || npm ci --no-audit --no-fund --silent; }

# The layout test's timing budgets (V0.38, V0.32) measure wall time and fail on a busy machine (#50), so the deploy
# gate skips them by default. CI runs `make test FYR_LAYOUT_TIMING=1`; `make test-timing` runs them on their own.
FYR_LAYOUT_TIMING ?= 0

test-web: web-deps
	cd web && npm run typecheck && npm test && npm run build && FYR_LAYOUT_TIMING=$(FYR_LAYOUT_TIMING) npm run test:layout && npm run test:auth

web-typecheck: web-deps
	cd web && npm run typecheck

web-unit: web-deps
	cd web && npm test

# Runs against the web/dist already built (make test builds it first).
web-layout: web-deps
	cd web && FYR_LAYOUT_TIMING=$(FYR_LAYOUT_TIMING) npm run test:layout

# Decision 0030 (4.5): sign-in in a real browser, against scratch fyrds and a fake tailscaled (internal/authtest).
# Needs web/dist built (the fyrd it builds embeds it) and Go on the PATH.
web-auth: web-deps
	cd web && npm run test:auth

test-timing: web-deps
	cd web && npm run build && FYR_LAYOUT_TIMING=1 npm run test:layout

# First install: put the build under ~/.local/share/fyr; then run `bin/fyr`.
install: build
	$(OUT)/fyr install $(OUT)

# Development without the launcher: serves the client from web/dist on disk.
run: go
	$(OUT)/fyrd serve -web web/dist
