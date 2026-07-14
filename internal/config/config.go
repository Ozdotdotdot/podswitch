// Package config holds daemon-wide constants and small config helpers.
package config

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/Ozdotdotdot/podswitch/internal/discovery"
)

// Headset describes the BlueZ and PipeWire names derived from a headset MAC.
// Each agent must set PODSWITCH_AIRPODS_MAC, normally through agent.env.
type Headset struct {
	MAC                string
	DevicePath         string
	PipeWireCard       string
	PipeWireSinkPrefix string
}

// CurrentHeadset reads PODSWITCH_AIRPODS_MAC and derives the BlueZ and
// PipeWire identifiers used by the agent. Keeping this out of source makes
// release builds safe to publish and reusable across machines.
func CurrentHeadset() (Headset, error) {
	mac := strings.ToUpper(os.Getenv("PODSWITCH_AIRPODS_MAC"))
	parsed, err := net.ParseMAC(mac)
	if err != nil || len(parsed) != 6 {
		return Headset{}, fmt.Errorf("PODSWITCH_AIRPODS_MAC must be a Bluetooth MAC such as AA:BB:CC:DD:EE:FF")
	}
	parts := strings.Split(mac, ":")
	underscore := strings.Join(parts, "_")
	return Headset{
		MAC:                mac,
		DevicePath:         "/org/bluez/hci0/dev_" + underscore,
		PipeWireCard:       "bluez_card." + underscore,
		PipeWireSinkPrefix: "bluez_output." + underscore,
	}, nil
}

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

// reachTimeout bounds the TCP probe used to pick between a discovered
// coordinator's Tailscale vs LAN address.
const reachTimeout = 1 * time.Second

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
// PODSWITCH_COORDINATOR env > cached config file > mDNS (LAN-only, ~3s). On
// mDNS, both the coordinator's Tailscale and LAN addresses are probed for
// reachability — a host that's also on the tailnet prefers the Tailscale
// address (stable across roaming); a LAN-only host (never joined the
// tailnet) falls back to the LAN address mDNS itself resolved. Whichever
// address actually connects is cached, so subsequent calls skip discovery
// entirely.
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

	found, err := discovery.Discover(context.Background(), mdnsDiscoverTimeout)
	if err != nil {
		return "", fmt.Errorf(
			"no coordinator configured: %w (pass -coordinator, set PODSWITCH_COORDINATOR, or run 'podswitchd config coordinator <host:port>')", err)
	}

	addr, err := pickReachable(found)
	if err != nil {
		return "", fmt.Errorf("found coordinator via mDNS but couldn't reach it at %s (tailscale) or %s (LAN): %w",
			found.TailscaleAddr, found.LANAddr, err)
	}
	if serr := saveUserConfig(userConfig{CoordinatorAddr: addr}); serr != nil {
		log.Printf("config: found coordinator at %s via mDNS but failed to cache it: %v", addr, serr)
	}
	return addr, nil
}

// pickReachable tries the Tailscale address first (stable once you roam),
// then falls back to the LAN address (works for hosts never on the
// tailnet, like a LAN-only Pi).
func pickReachable(found discovery.Found) (string, error) {
	for _, addr := range []string{found.TailscaleAddr, found.LANAddr} {
		if addr == "" {
			continue
		}
		conn, err := net.DialTimeout("tcp", addr, reachTimeout)
		if err == nil {
			conn.Close()
			return addr, nil
		}
	}
	return "", fmt.Errorf("neither address accepted a connection")
}
