BINARIES := podswitchd podswitch
DIST_DIR := dist
MODULE := github.com/Ozdotdotdot/podswitch

.PHONY: all build arm64 amd64 dist tidy clean

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

# dist creates the two release archives consumed by deploy/install.sh. Keep
# the archive names stable so GitHub's releases/latest/download URL works.
dist:
	rm -rf $(DIST_DIR)
	mkdir -p $(DIST_DIR)/podswitch_linux_amd64 $(DIST_DIR)/podswitch_linux_arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o $(DIST_DIR)/podswitch_linux_amd64/podswitchd ./cmd/podswitchd
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o $(DIST_DIR)/podswitch_linux_amd64/podswitch ./cmd/podswitch
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o $(DIST_DIR)/podswitch_linux_arm64/podswitchd ./cmd/podswitchd
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o $(DIST_DIR)/podswitch_linux_arm64/podswitch ./cmd/podswitch
	cp deploy/install.sh $(DIST_DIR)/podswitch_linux_amd64/install.sh
	cp deploy/install.sh $(DIST_DIR)/podswitch_linux_arm64/install.sh
	tar -C $(DIST_DIR) -czf $(DIST_DIR)/podswitch_linux_amd64.tar.gz podswitch_linux_amd64
	tar -C $(DIST_DIR) -czf $(DIST_DIR)/podswitch_linux_arm64.tar.gz podswitch_linux_arm64
	rm -rf $(DIST_DIR)/podswitch_linux_amd64 $(DIST_DIR)/podswitch_linux_arm64
	sha256sum $(DIST_DIR)/podswitch_linux_*.tar.gz > $(DIST_DIR)/checksums.txt

tidy:
	go mod tidy

clean:
	rm -rf bin $(DIST_DIR)
