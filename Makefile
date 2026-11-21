APP        := debt-reducer
BIN        := bin/$(APP)
DB         ?= data/debt-reducer.db
ADDR       ?= localhost:8080
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
