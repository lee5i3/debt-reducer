APP        := debt-reducer
BIN        := bin/$(APP)
DB         ?= data/debt-reducer.db
ADDR       ?= localhost:8080
PORT       := $(lastword $(subst :, ,$(ADDR)))
LOG        := bin/$(APP).log
TEMPL      := go tool templ
# The app version comes from git tags (v1.2.3). CI works out the next version
# from commit messages and passes VERSION=...; local builds use the latest tag
# (plus commits since, e.g. 0.1.0-3-gabc1234), or 0.0.0-dev with no tags.
VERSION    ?= $(or $(patsubst v%,%,$(shell git describe --tags --match 'v[0-9]*' 2>/dev/null)),0.0.0-dev)
LDFLAGS    := -s -w -X github.com/lee5i3/debt-reducer/internal/version.Version=$(VERSION)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: init
init: ## Download deps and tools, create data dir, generate templ code
	go mod download
	mkdir -p $(dir $(DB))
	$(MAKE) generate

.PHONY: generate
generate: ## Generate Go code from .templ files
	$(TEMPL) generate

.PHONY: build
build: generate ## Build the binary to bin/
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/$(APP)

.PHONY: dev
dev: ## Run with live reload (open http://localhost:7331)
	$(TEMPL) generate --watch \
		--proxy="http://$(ADDR)" \
		--open-browser=false \
		--watch-pattern='(.+\.go$$)|(.+\.templ$$)|(.+\.css$$)' \
		--cmd="go run ./cmd/$(APP) -addr $(ADDR) -db $(DB)"

.PHONY: run
run: build ## Build and run the binary
	./$(BIN) -addr $(ADDR) -db $(DB)

.PHONY: stop
stop: ## Stop whatever is serving the app port (make run, make restart or go run)
	@pids=$$(lsof -t -iTCP:$(PORT) -sTCP:LISTEN 2>/dev/null); \
	if [ -n "$$pids" ]; then kill $$pids && echo "stopped pid $$pids on :$(PORT)"; else echo "nothing running on :$(PORT)"; fi
	@for i in $$(seq 1 50); do lsof -t -iTCP:$(PORT) -sTCP:LISTEN >/dev/null 2>&1 || exit 0; sleep 0.1; done; \
	echo "port :$(PORT) is still busy" >&2; exit 1

.PHONY: restart
restart: build stop ## Rebuild, stop the running app and start it in the background
	@nohup ./$(BIN) -addr $(ADDR) -db $(DB) > $(LOG) 2>&1 &
	@for i in $$(seq 1 50); do curl -sf http://$(ADDR)/healthz >/dev/null && echo "running at http://$(ADDR) (log: $(LOG))" && exit 0; sleep 0.2; done; \
	echo "app did not start, see $(LOG)" >&2; tail -n 20 $(LOG) >&2; exit 1

.PHONY: desktop
desktop: build desktop/node_modules ## Open the desktop app (desktop data folder, or DEBT_REDUCER_DB)
	cd desktop && env -u ELECTRON_RUN_AS_NODE npx electron .   # VS Code terminals set it

desktop/node_modules: desktop/package.json desktop/package-lock.json
	cd desktop && npm ci
	@touch $@

# GOOS/GOARCH/electron-builder folder (${os}-${arch}) for each desktop build
DESKTOP_TARGETS := linux/amd64/linux-x64 linux/arm64/linux-arm64 \
	windows/amd64/win-x64 windows/arm64/win-arm64 \
	darwin/amd64/mac-x64 darwin/arm64/mac-arm64

.PHONY: desktop-bins
desktop-bins: generate ## Cross-compile the server for every desktop platform into bin/desktop/
	@set -e; for t in $(DESKTOP_TARGETS); do \
		os=$${t%%/*}; rest=$${t#*/}; arch=$${rest%%/*}; dir=$${rest#*/}; \
		ext=""; if [ "$$os" = windows ]; then ext=".exe"; fi; \
		echo "  $$dir"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags="$(LDFLAGS)" -o bin/desktop/$$dir/$(APP)$$ext ./cmd/$(APP); \
	done

.PHONY: desktop-dist
desktop-dist: desktop-bins desktop/node_modules ## Package installers into bin/dist/ (PLATFORMS="--win nsis", ARCHS="--x64"; macOS needs a Mac)
	cd desktop && env -u ELECTRON_RUN_AS_NODE npx electron-builder $(or $(PLATFORMS),--linux) $(or $(ARCHS),--x64 --arm64) \
		--publish never -c.extraMetadata.version=$(VERSION)

.PHONY: version
version: ## Print the app version (from git tags, or VERSION=...)
	@echo $(VERSION)

.PHONY: logs
logs: ## Follow the log of the app started by make restart
	tail -f $(LOG)

.PHONY: test
test: generate ## Run tests (Go, plus the desktop app's when node is installed)
	go test ./...
	@if command -v node >/dev/null 2>&1; then cd desktop && node --test --test-reporter=dot; fi

# govulncheck: the Go team's vulnerability checker. It reports only
# vulnerabilities in code the app actually calls. Pinned; go run verifies it
# against the Go checksum database.
GOVULNCHECK_VERSION := v1.10.0

.PHONY: security
security: generate ## Check Go and npm dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	cd desktop && npm audit --audit-level=moderate

.PHONY: lint
lint: generate ## Format check and vet
	$(TEMPL) fmt -fail .
	go vet ./...

.PHONY: fmt
fmt: ## Format Go and templ files
	$(TEMPL) fmt .
	gofmt -w .

.PHONY: clean
clean: ## Remove build output and generated code (keeps your database)
	rm -rf bin
	find . -name '*_templ.go' -delete

.PHONY: db-reset
db-reset: ## Delete the local SQLite database
	rm -f $(DB) $(DB)-wal $(DB)-shm
