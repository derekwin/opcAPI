.PHONY: run build test fmt

run:
	go run ./cmd/api

build:
	mkdir -p bin
	go build -o bin/opcapi ./cmd/api

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal
