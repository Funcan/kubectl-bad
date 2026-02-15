BINARY  := kubectl-bad
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build clean format lint test show-coverage

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

clean:
	rm -f $(BINARY) coverage.out

format:
	gofmt -w .

lint:
	golangci-lint run ./...

test:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

show-coverage: test
	go tool cover -html=coverage.out
