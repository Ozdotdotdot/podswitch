// Package config holds daemon-wide constants and small config helpers.
package config

import "os"

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
