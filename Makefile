BIN     := bin/delphi
VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X delphi.dev/delphi/internal/version.Version=$(VERSION)

.PHONY: build test vet check run clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/delphi

test:
	go test ./...

vet:
	go vet ./...

check: vet test build

run:
	go run ./cmd/delphi panel --config panel.example.toml

clean:
	rm -rf bin
