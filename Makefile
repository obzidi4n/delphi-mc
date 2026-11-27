BIN     := bin/delphi
VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X delphi.dev/delphi/internal/version.Version=$(VERSION)

.PHONY: build test vet lint config-check check run clean generate generate-check bridge

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/delphi

test:
	go test ./...

vet:
	go vet ./...

lint: vet config-check

config-check:
	go run ./cmd/check-config

bridge:
	./plugin/bridge/build.sh

generate:
	go run ./cmd/gen-api

generate-check:
	go run ./cmd/gen-api -check

check: lint test build generate-check

run:
	go run ./cmd/delphi panel --config panel.example.toml

clean:
	rm -rf bin
