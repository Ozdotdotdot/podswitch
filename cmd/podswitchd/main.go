// Command podswitchd is the podswitch daemon: it runs as either the
// coordinator (always-on switch server) or an agent (audio-endpoint host).
// It never doubles as a CLI — see cmd/podswitch for that.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/grandcat/zeroconf"

	"github.com/Ozdotdotdot/podswitch/internal/agent"
	"github.com/Ozdotdotdot/podswitch/internal/config"
	"github.com/Ozdotdotdot/podswitch/internal/coordinator"
	"github.com/Ozdotdotdot/podswitch/internal/discovery"
)

func main() {
	mode := flag.String("mode", "", "role: agent|coordinator (required)")
	addr := flag.String("addr", config.DefaultCoordinatorAddr, "coordinator: listen address")
	coordinatorAddr := flag.String("coordinator", "", "agent: coordinator host:port (or ws(s):// URL); resolved via env/cache/mDNS if unset")
	host := flag.String("host", config.Hostname(), "agent: identity reported to the coordinator")
	flag.Parse()

	switch *mode {
	case "coordinator":
		runCoordinator(*addr)
	case "agent":
		coord, err := config.ResolveCoordinatorAddr(*coordinatorAddr)
		if err != nil {
			log.Fatalf("podswitchd: %v", err)
		}
		runAgent(*host, coord)
	default:
		fmt.Fprintln(os.Stderr, "podswitchd: -mode is required and must be \"agent\" or \"coordinator\"")
		os.Exit(1)
	}
}

func runCoordinator(addr string) {
	c := coordinator.New()
	srv := &http.Server{Addr: addr, Handler: c.Handler()}

	mdnsServer := advertiseCoordinator(addr)
	if mdnsServer != nil {
		defer mdnsServer.Shutdown()
	}

	go func() {
		log.Printf("podswitchd: coordinator listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	waitForShutdown()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// advertiseCoordinator publishes the coordinator on mDNS so agents/CLI on
// the same LAN can find it without configuration. The advertised address is
// the Tailscale IP (not the mDNS-resolved LAN IP), so it keeps working once
// a client roams off the LAN and relies on its cached copy. Best-effort:
// logs and returns nil if `tailscale` isn't available or mDNS fails.
func advertiseCoordinator(listenAddr string) *zeroconf.Server {
	tsIP, err := tailscaleIP()
	if err != nil {
		log.Printf("podswitchd: mDNS advertise skipped (no tailscale IP): %v", err)
		return nil
	}
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		log.Printf("podswitchd: mDNS advertise skipped (bad -addr %q): %v", listenAddr, err)
		return nil
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		log.Printf("podswitchd: mDNS advertise skipped (bad port %q): %v", port, err)
		return nil
	}

	srv, err := discovery.Advertise(config.Hostname(), portNum, net.JoinHostPort(tsIP, port))
	if err != nil {
		log.Printf("podswitchd: mDNS advertise failed: %v", err)
		return nil
	}
	log.Printf("podswitchd: advertising on mDNS as %q (%s)", config.Hostname(), net.JoinHostPort(tsIP, port))
	return srv
}

func tailscaleIP() (string, error) {
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", fmt.Errorf("empty tailscale ip")
	}
	return ip, nil
}

func runAgent(host, coordinatorAddr string) {
	a, err := agent.New(host, agent.BuildCoordinatorURL(coordinatorAddr))
	if err != nil {
		log.Fatalf("podswitchd: init agent: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		waitForShutdown()
		cancel()
	}()

	log.Printf("podswitchd: agent %q dialing coordinator %s", host, a.CoordinatorURL)
	a.Run(ctx)
}

func waitForShutdown() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("podswitchd: shutting down...")
}
