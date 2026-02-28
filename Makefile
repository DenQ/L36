BINARY_NAME=l36
MAIN_PACKAGE=./cmd/l36

.PHONY: build run test race cover clean dev

build:
	mkdir -p bin
	go build -o bin/$(BINARY_NAME) $(MAIN_PACKAGE)

run:
	go run $(MAIN_PACKAGE)

dev:
	air

test:
	go test -v ./internal/**

# bench:
# 	go test -v -bench=. ./internal/storage

race:
	go test -v -race ./...

cover:
	go test -cover ./...

clean:
	go clean -cache
	rm -rf bin/
	rm -rf tmp/

install:
	go mod tidy