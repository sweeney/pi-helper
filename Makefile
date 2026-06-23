.PHONY: build build-pi-zero build-pi-zero2 build-arm64 build-all test lint clean deploy

# Default build for current platform
build:
	go build -o dist/pi-helper ./cmd/pi-helper

# Build for Pi Zero (ARMv6)
build-pi-zero:
	GOOS=linux GOARCH=arm GOARM=6 go build -o dist/pi-helper-armv6 ./cmd/pi-helper

# Build for Pi Zero 2 and newer 32-bit OS (ARMv7)
build-pi-zero2:
	GOOS=linux GOARCH=arm GOARM=7 go build -o dist/pi-helper-armv7 ./cmd/pi-helper

# Build for 64-bit Pi OS (Pi 3/4/5, arm64)
build-arm64:
	GOOS=linux GOARCH=arm64 go build -o dist/pi-helper-arm64 ./cmd/pi-helper

# Build all Pi variants
build-all: build-pi-zero build-pi-zero2 build-arm64

# Run tests
test:
	go test -v ./...

# Run linter
lint:
	golangci-lint run

# Clean build artifacts
clean:
	rm -rf dist/

# Deploy to remote host (auto-detects target arch). Usage: make deploy HOST=user@host
deploy:
	@test -n "$(HOST)" || (echo "Usage: make deploy HOST=user@host" && exit 1)
	@chmod +x deploy/deploy.sh
	./deploy/deploy.sh $(HOST)
