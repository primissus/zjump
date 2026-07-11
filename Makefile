.PHONY: build dev test test-all lint fmt clean install run

BINARY := zjump
BIN_DIR := bin

build:
	go build -o $(BINARY) ./cmd/zjump

dev:
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/zjump

run: dev
	$(BIN_DIR)/$(BINARY) $(ARGS)

test:
	go test ./...

test-all:
	go test -tags shelltests ./...

lint:
	gofmt -l .
	go vet ./...

fmt:
	gofmt -w .

install: build
	scripts/install.sh

clean:
	rm -f $(BINARY)
	rm -rf $(BIN_DIR)
