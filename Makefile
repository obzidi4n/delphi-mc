BIN     := bin/delphi
VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X delphi.dev/delphi/internal/version.Version=$(VERSION)

.PHONY: build test vet check run clean generate generate-check bridge

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/delphi

test:
	go test ./...

vet:
	go vet ./...

bridge:
	./plugin/bridge/build.sh

generate:
	go run ./cmd/gen-api

generate-check:
	go run ./cmd/gen-api -check

check: vet test build generate-check

run:
	go run ./cmd/delphi panel --config panel.example.toml

clean:
	rm -rf bin
