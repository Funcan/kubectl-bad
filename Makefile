BINARY  := kubectl-bad
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Go 1.25 removed covdata from the toolchain; ensure it's available for coverage
GOROOT_TOOL_DIR := $(shell go env GOROOT)/pkg/tool/$(shell go env GOOS)_$(shell go env GOARCH)
COVDATA         := $(GOROOT_TOOL_DIR)/covdata

.PHONY: default build clean format lint test show-coverage ensure-covdata release snapshot

default: format lint test build

$(COVDATA):
	@chmod u+w "$(GOROOT_TOOL_DIR)" 2>/dev/null || true
	go build -o "$@" cmd/covdata

ensure-covdata: $(COVDATA)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

clean:
	rm -f $(BINARY) coverage.out

format:
	gofmt -w .

lint:
	golangci-lint run ./...

test: ensure-covdata
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

show-coverage: test
	go tool cover -html=coverage.out

release:
	goreleaser release --clean

snapshot:
	goreleaser release --snapshot --clean
