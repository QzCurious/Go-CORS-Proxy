.PHONY: build test cross-build demo-simple demo-preflight

build:
	go build -o bin/seamless-cors ./cmd/seamless-cors

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
