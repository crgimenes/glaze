.PHONY: all build vet test test-integration lint fmt tidy examples

# Default: the checks CI runs for the library.
all: build vet test

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

# GUI integration test. Needs a display; on Linux run under xvfb-run.
test-integration:
	go test -tags=integration -run TestWebview .

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
