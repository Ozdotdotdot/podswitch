// Package discovery lets an agent/CLI find the coordinator via mDNS instead
// of requiring -coordinator on every invocation. mDNS multicast doesn't
// cross Tailscale (it's LAN-only), so the coordinator advertises its
// Tailscale-reachable address in a TXT record for clients that roam off
// the LAN — but a client that's LAN-only and never joined the tailnet
// (e.g. the Pi) can't reach that address, only the plain LAN IP/port mDNS
// itself resolved. Discover returns both; the caller probes which one is
// actually reachable (see config.ResolveCoordinatorAddr).
package discovery

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	ServiceType   = "_podswitch._tcp"
	ServiceDomain = "local."
	txtAddrPrefix = "addr="
)

// Found is a discovered coordinator, with both candidate addresses a client
// might need depending on whether it's on the tailnet.
type Found struct {
	TailscaleAddr string // from the TXT record; stable across roaming
	LANAddr       string // from the raw mDNS response; reachable even off-tailnet
}

// Advertise registers the coordinator on mDNS. instance is a human-readable
// name (e.g. the hostname); tailscaleAddr is published via TXT record so
// roaming clients can reach it even after they leave the LAN.
func Advertise(instance string, port int, tailscaleAddr string) (*zeroconf.Server, error) {
	return zeroconf.Register(instance, ServiceType, ServiceDomain, port,
		[]string{"version=1", txtAddrPrefix + tailscaleAddr}, nil)
}

// Discover browses for a coordinator on the local network for up to
// timeout. Returns an error if none is found in time (the caller should
// fall back to a cached/manually-configured address, since discovery only
// works on the same LAN as the coordinator).
func Discover(ctx context.Context, timeout time.Duration) (Found, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return Found{}, fmt.Errorf("mdns resolver: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	entries := make(chan *zeroconf.ServiceEntry, 8)
	if err := resolver.Browse(ctx, ServiceType, ServiceDomain, entries); err != nil {
		return Found{}, fmt.Errorf("mdns browse: %w", err)
	}

	for entry := range entries {
		tsAddr, ok := addrFromTXT(entry.Text)
		if !ok {
			continue
		}
		found := Found{TailscaleAddr: tsAddr}
		if len(entry.AddrIPv4) > 0 {
			found.LANAddr = net.JoinHostPort(entry.AddrIPv4[0].String(), strconv.Itoa(entry.Port))
		}
		return found, nil
	}
	return Found{}, fmt.Errorf("no podswitch coordinator found on the local network within %s", timeout)
}

func addrFromTXT(txt []string) (string, bool) {
	for _, t := range txt {
		if strings.HasPrefix(t, txtAddrPrefix) {
			return strings.TrimPrefix(t, txtAddrPrefix), true
		}
	}
	return "", false
}
