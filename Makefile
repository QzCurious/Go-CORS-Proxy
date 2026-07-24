.PHONY: build test cross-build demo-simple demo-preflight

VERSION ?= dev
LDFLAGS := -s -w -X seamless-cors/internal/version.Version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/seamless-cors ./cmd/seamless-cors

test:
	go test ./...

demo-simple:
	go run ./demo/cors-simple-requests

demo-preflight:
	go run ./demo/cors-preflighted-requests

cross-build:
	GOOS=darwin GOARCH=amd64 go build ./...
	GOOS=darwin GOARCH=arm64 go build ./...
	GOOS=windows GOARCH=amd64 go build ./...
	GOOS=windows GOARCH=arm64 go build ./...
