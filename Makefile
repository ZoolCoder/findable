# Everything here runs offline except docs. No test in this repository touches
# a network.

BIN := bin/findable
# local builds carry the git version with a -dev suffix
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')-dev
LDFLAGS := -X main.version=$(VERSION)

.PHONY: all build run ui test test-unit race cover lint fmt install evals docs docs-serve clean

all: lint test build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/findable

run: build
	$(BIN) $(ARGS)

ui: build
	$(BIN) ui

test:
	go test ./...

test-unit:
	go test -short ./...

race:
	go test -race ./...

# -coverpkg: most code is exercised through the CLI tests in cmd/, which
# per-package counting would not credit.
cover:
	go test -coverpkg=./... -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "not gofmt-clean"; exit 1; }
	go vet ./...
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed, go vet only"

fmt:
	gofmt -w .

install: build
	mkdir -p $(HOME)/.local/bin
	cp $(BIN) $(HOME)/.local/bin/findable.new
	mv $(HOME)/.local/bin/findable.new $(HOME)/.local/bin/findable
	@echo "installed $(VERSION) to ~/.local/bin/findable (add to PATH if needed)"

evals: ## run the plugin eval suite (evals/), needs the claude CLI
	claude plugin eval . --no-publish --threshold 0.8

docs:
	[ -d node_modules ] || npm ci
	npm run docs

docs-serve: docs
	npx --yes http-server build/site -p 8080 -c-1

clean:
	rm -rf bin dist build coverage.out
