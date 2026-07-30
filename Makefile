.PHONY: build dev test lint clean

VERSION := $(shell cat VERSION 2>/dev/null || echo "0.1.0")

build:
	CGO_ENABLED=0 go build -ldflags "-X 'main.Version=$(VERSION)'" -o dist/marvo .

dev:
	go run . -c config.yaml

test:
	go test ./...

lint:
	golangci-lint run

clean:
	rm -rf dist/
