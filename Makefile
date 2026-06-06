.PHONY: all build test clean generate

all: generate clean build

run:
	go run cmd/trading212-exporter/main.go

build:
	go build -o trading212-exporter cmd/trading212-exporter/main.go

test:
	go test -v ./...

generate:
	go generate ./...

clean:
	rm -f trading212-exporter
