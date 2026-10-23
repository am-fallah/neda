.PHONY: build test race vet fmt fmt-check check run

build:
	go build ./...

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	test -z "$$(gofmt -l .)"

check: fmt-check vet test build

run:
	NEDA_ENV=development go run ./cmd/neda serve
