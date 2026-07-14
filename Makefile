BINARY := podswitchd
PKG := ./cmd/podswitchd

.PHONY: all build arm64 tidy clean

all: build

build:
	go build -o bin/$(BINARY) $(PKG)

# Cross-compile for the Pi (aarch64, cgo-free) — same binary also runs on the
# switch server / workstation / laptop, all amd64/arm64 depending on host.
arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/$(BINARY)-arm64 $(PKG)

amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/$(BINARY)-amd64 $(PKG)

tidy:
	go mod tidy

clean:
	rm -rf bin
