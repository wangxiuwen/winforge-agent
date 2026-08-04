VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -s -w -X main.version=$(VERSION)

.PHONY: test build release

test:
	go test ./...
	go vet ./...

build:
	mkdir -p dist
	go build -trimpath -ldflags="$(LDFLAGS)" -o dist/winforge ./cmd/winforge

release:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/winforge-windows-amd64.exe ./cmd/winforge
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/winforge-windows-arm64.exe ./cmd/winforge
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/winforge-darwin-amd64 ./cmd/winforge
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/winforge-darwin-arm64 ./cmd/winforge
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/winforge-linux-amd64 ./cmd/winforge
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/winforge-linux-arm64 ./cmd/winforge
	cd dist && shasum -a 256 winforge-* > SHA256SUMS
