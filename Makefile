BINARY_NAME=l36
MAIN_PACKAGE=./cmd/l36

.PHONY: build run test race cover clean dev seed view

seed:
	go run ./cmd/seed

view:
	@test -n "$(N)" || (echo "usage: make view N=0" && exit 1)
	go run ./cmd/view -n $(N)

build:
	mkdir -p bin
	go build -o bin/$(BINARY_NAME) $(MAIN_PACKAGE)

run:
	go run $(MAIN_PACKAGE)

dev:
	air

test:
	go test -v ./internal/**

stress:
	go test -v -count=1 ./tests/stress

bench:
	go test -v -bench=. ./internal/storage

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