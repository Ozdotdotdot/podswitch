BINARIES := podswitchd podswitch

.PHONY: all build arm64 amd64 tidy clean

all: build

build:
	go build -o bin/podswitchd ./cmd/podswitchd
	go build -o bin/podswitch ./cmd/podswitch

# Cross-compile for the Pi (aarch64, cgo-free) — same binaries also run on
# the switch server / workstation / laptop, all amd64/arm64 depending on host.
arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/podswitchd-arm64 ./cmd/podswitchd
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/podswitch-arm64 ./cmd/podswitch

amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/podswitchd-amd64 ./cmd/podswitchd
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/podswitch-amd64 ./cmd/podswitch

tidy:
	go mod tidy

clean:
	rm -rf bin
