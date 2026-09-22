SCHEMA_URL ?= https://docs.trading212.com/_bundle/api.yaml?download
SCHEMA_FILE ?= pkg/gen/api.yaml

.PHONY: all build test clean generate update-schema

all: generate clean build

run:
	go run cmd/trading212-exporter/main.go

build:
	go build -o trading212-exporter cmd/trading212-exporter/main.go

test:
	go test -v ./...

generate:
	go generate ./...

update-schema:
	curl --fail --location --silent --show-error --output "$(SCHEMA_FILE).tmp" "$(SCHEMA_URL)"
	mv "$(SCHEMA_FILE).tmp" "$(SCHEMA_FILE)"
	$(MAKE) generate

clean:
	rm -f trading212-exporter
