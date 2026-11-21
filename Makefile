APP        := debt-reducer
BIN        := bin/$(APP)
DB         ?= data/debt-reducer.db
ADDR       ?= localhost:8080
PORT       := $(lastword $(subst :, ,$(ADDR)))
LOG        := bin/$(APP).log
TEMPL      := go tool templ

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
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/$(APP)

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

.PHONY: logs
logs: ## Follow the log of the app started by make restart
	tail -f $(LOG)

.PHONY: test
test: generate ## Run tests
	go test ./...

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
