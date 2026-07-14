// Package config holds daemon-wide constants and small config helpers.
package config

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/Ozdotdotdot/podswitch/internal/discovery"
)

// AirPodsMAC is the AirPods Max MAC address, constant across every host.
const AirPodsMAC = "AA:BB:CC:DD:EE:FF"

// AirPodsDevicePath is the BlueZ D-Bus object path for the AirPods, constant
// across every host (all on hci0).
const AirPodsDevicePath = "/org/bluez/hci0/dev_REDACTED_MAC"

// PipeWireCard is the PipeWire card name for the AirPods.
const PipeWireCard = "bluez_card.REDACTED_MAC"

// PipeWireSinkPrefix is the stable prefix of the AirPods' PipeWire sink name
// (the suffix after the MAC is unstable — always prefix-match).
const PipeWireSinkPrefix = "bluez_output.REDACTED_MAC"

// DefaultCoordinatorAddr is the coordinator's HTTP/WS listen address.
// (9090 collides with Prometheus on the switch server; 8090 groups with
// switch-alarm's 8080.)
const DefaultCoordinatorAddr = ":8090"

// Hostname returns the local hostname, used as the default agent identity.
func Hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown-host"
	}
	return h
}

// mdnsDiscoverTimeout bounds how long a caller waits for mDNS before
// falling back to an error — mDNS only succeeds when on the coordinator's
// LAN, so this should stay short.
const mdnsDiscoverTimeout = 3 * time.Second

// userConfig is the cached coordinator address, written once discovery (or
// an explicit -coordinator) succeeds so future runs don't need either.
type userConfig struct {
	CoordinatorAddr string `toml:"coordinator_addr"`
}

// UserConfigPath returns ~/.config/podswitch/config.toml.
func UserConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME")
	}
	return filepath.Join(dir, "podswitch", "config.toml")
}

func loadUserConfig() userConfig {
	var uc userConfig
	data, err := os.ReadFile(UserConfigPath())
	if err != nil {
		return uc
	}
	_ = toml.Unmarshal(data, &uc)
	return uc
}

func saveUserConfig(uc userConfig) error {
	path := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(uc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SetCoordinatorAddr writes addr to the cached user config (used by the
// `podswitchd config coordinator <addr>` CLI to define it once).
func SetCoordinatorAddr(addr string) error {
	return saveUserConfig(userConfig{CoordinatorAddr: addr})
}

// ResolveCoordinatorAddr finds the coordinator's host:port without the
// caller having to pass -coordinator every time. Priority: explicit flag >
// PODSWITCH_COORDINATOR env > cached config file > mDNS (LAN-only, ~3s). A
// successful mDNS discovery is cached to the config file so subsequent
// calls (including once you've roamed off the LAN) skip straight to the
// cache.
func ResolveCoordinatorAddr(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if v := os.Getenv("PODSWITCH_COORDINATOR"); v != "" {
		return v, nil
	}
	if uc := loadUserConfig(); uc.CoordinatorAddr != "" {
		return uc.CoordinatorAddr, nil
	}

	addr, err := discovery.Discover(context.Background(), mdnsDiscoverTimeout)
	if err != nil {
		return "", fmt.Errorf(
			"no coordinator configured: %w (pass -coordinator, set PODSWITCH_COORDINATOR, or run 'podswitchd config coordinator <host:port>')", err)
	}
	if serr := saveUserConfig(userConfig{CoordinatorAddr: addr}); serr != nil {
		log.Printf("config: found coordinator at %s via mDNS but failed to cache it: %v", addr, serr)
	}
	return addr, nil
}
