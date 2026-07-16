// Package aacp observes AirPods routing state over Apple's private L2CAP service.
package aacp

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const psm = 0x1001

var (
	header               = mustHex("04000400")
	handshake            = mustHex("00000400010002000000000000000000")
	handshakeAck         = mustHex("01000400")
	setFeatures          = mustHex("040004004d00d700000000000000")
	requestNotifications = mustHex("040004000f00ffffffffff")
)

// SourceEvent identifies the AirPods' current audio source. Type is none,
// call, media, or unknown.
type SourceEvent struct {
	MAC  string
	Type string
	At   time.Time
}

// Observe connects to an already-connected AirPods device and emits routing
// notifications until the context is cancelled or the L2CAP link closes.
// It sends only the handshake, feature flags, and notification subscription.
func Observe(ctx context.Context, airpodsMAC string, emit func(SourceEvent)) error {
	addr, err := bluetoothAddress(airpodsMAC)
	if err != nil {
		return err
	}
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, unix.BTPROTO_L2CAP)
	if err != nil {
		return fmt.Errorf("open L2CAP socket: %w", err)
	}
	defer unix.Close(fd)

	closed := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = unix.Shutdown(fd, unix.SHUT_RDWR)
		case <-closed:
		}
	}()
	defer close(closed)

	if err := unix.Connect(fd, &unix.SockaddrL2{PSM: psm, Addr: addr}); err != nil {
		return fmt.Errorf("connect AACP L2CAP: %w", err)
	}
	if err := writePacket(fd, handshake); err != nil {
		return err
	}

	featuresSent := false
	notificationsSent := false
	buf := make([]byte, 4096)
	for {
		n, err := unix.Read(fd, buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, unix.EBADF) || errors.Is(err, unix.ENOTCONN) {
				return ctx.Err()
			}
			return fmt.Errorf("read AACP packet: %w", err)
		}
		if n == 0 {
			return nil
		}
		packet := buf[:n]
		if event, ok := ParseSource(packet, time.Now()); ok {
			emit(event)
		}
		if !featuresSent && len(packet) >= len(handshakeAck) && string(packet[:len(handshakeAck)]) == string(handshakeAck) {
			if err := writePacket(fd, setFeatures); err != nil {
				return err
			}
			featuresSent = true
		} else if featuresSent && !notificationsSent {
			if err := writePacket(fd, requestNotifications); err != nil {
				return err
			}
			notificationsSent = true
		}
	}
}

// ParseSource parses an AUDIO_SOURCE (opcode 0x0e) notification.
func ParseSource(packet []byte, at time.Time) (SourceEvent, bool) {
	if len(packet) < 13 || string(packet[:4]) != string(header) || packet[4] != 0x0e {
		return SourceEvent{}, false
	}
	mac := make([]string, 0, 6)
	for i := 11; i >= 6; i-- {
		mac = append(mac, fmt.Sprintf("%02X", packet[i]))
	}
	typ := map[byte]string{0: "none", 1: "call", 2: "media"}[packet[12]]
	if typ == "" {
		typ = "unknown"
	}
	return SourceEvent{MAC: strings.Join(mac, ":"), Type: typ, At: at}, true
}

func bluetoothAddress(mac string) ([6]uint8, error) {
	var out [6]uint8
	parsed, err := net.ParseMAC(mac)
	if err != nil || len(parsed) != 6 {
		return out, fmt.Errorf("invalid Bluetooth address %q", mac)
	}
	copy(out[:], parsed)
	return out, nil
}

func writePacket(fd int, packet []byte) error {
	if err := unix.Send(fd, packet, 0); err != nil {
		return fmt.Errorf("write AACP packet: %w", err)
	}
	return nil
}

func mustHex(value string) []byte {
	decoded, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}
	return decoded
}
