// Package discovery lets an agent/CLI find the coordinator via mDNS instead
// of requiring -coordinator on every invocation. mDNS multicast doesn't
// cross Tailscale (it's LAN-only), so the coordinator advertises its
// Tailscale-reachable address in a TXT record — a client that discovers it
// once on the home LAN caches that same address, which keeps working away
// from home too (see config.LoadCachedCoordinator/SaveCachedCoordinator).
package discovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	ServiceType   = "_podswitch._tcp"
	ServiceDomain = "local."
	txtAddrPrefix = "addr="
)

// Advertise registers the coordinator on mDNS. instance is a human-readable
// name (e.g. the hostname); addr is the Tailscale host:port clients should
// actually dial (published via TXT record, not derived from the mDNS
// response, since the LAN-resolved IP wouldn't be reachable once roaming).
func Advertise(instance string, port int, addr string) (*zeroconf.Server, error) {
	return zeroconf.Register(instance, ServiceType, ServiceDomain, port,
		[]string{"version=1", txtAddrPrefix + addr}, nil)
}

// Discover browses for a coordinator on the local network for up to
// timeout and returns its advertised Tailscale host:port. Returns an error
// if none is found in time (the caller should fall back to a cached/
// manually-configured address, since discovery only works on the same LAN
// as the coordinator).
func Discover(ctx context.Context, timeout time.Duration) (string, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return "", fmt.Errorf("mdns resolver: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	entries := make(chan *zeroconf.ServiceEntry, 8)
	if err := resolver.Browse(ctx, ServiceType, ServiceDomain, entries); err != nil {
		return "", fmt.Errorf("mdns browse: %w", err)
	}

	for entry := range entries {
		if addr, ok := addrFromTXT(entry.Text); ok {
			return addr, nil
		}
	}
	return "", fmt.Errorf("no podswitch coordinator found on the local network within %s", timeout)
}

func addrFromTXT(txt []string) (string, bool) {
	for _, t := range txt {
		if strings.HasPrefix(t, txtAddrPrefix) {
			return strings.TrimPrefix(t, txtAddrPrefix), true
		}
	}
	return "", false
}
