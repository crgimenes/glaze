.PHONY: all build vet test test-short lint fmt tidy examples

# Default: the checks CI runs for the library.
all: build vet test

build:
	go build ./...

vet:
	go vet ./...

# Full suite, including the per-OS GUI scenarios. Bounded with a timeout so a
# wedged GUI test fails fast instead of running to go test's 10m default. On
# Linux the GUI tests need a display; run under xvfb-run.
test:
	go test -timeout 180s ./...

# Fast, headless run: TestMain honors -short and skips the GUI scenarios.
test-short:
	go test -short ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

# Tidy both modules (the library and the separate examples module).
tidy:
	go mod tidy
	cd examples && go mod tidy

# Build the example programs (their own module).
examples:
	cd examples && go build ./...
