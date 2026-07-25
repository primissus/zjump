.PHONY: help build dev run test test-all lint fmt install clean

BINARY := zjump
BIN_DIR := bin

build:        ## Build binary to repo root (default; also: make help)
	go build -o $(BINARY) ./cmd/zjump

help:         ## Print this help message and exit
	@printf '\033[36mUsage: make <target>\033[0m\n\n'
	@awk -F '## ' '/^[a-z_-]+:/ { printf "  \033[33m%-12s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

dev:          ## Build binary to bin/ directory
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/zjump

run: dev      ## Run with ARGS: make run ARGS="init bash"
	$(BIN_DIR)/$(BINARY) $(ARGS)

test:         ## Run dependency-free tests
	go test ./...

test-all:     ## Run all tests including shell/git integration
	go test -tags shelltests ./...

lint:         ## Check formatting + vet
	gofmt -l .
	go vet ./...

fmt:          ## Format all Go source
	gofmt -w .

install: build ## Build and run scripts/install.sh
	scripts/install.sh

clean:        ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf $(BIN_DIR)
