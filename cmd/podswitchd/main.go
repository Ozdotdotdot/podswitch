// Command podswitchd is the podswitch daemon: it runs as either the
// coordinator (always-on switch server) or an agent (audio-endpoint host).
// Its only maintenance subcommand is "podswitchd update", which replaces the
// local release binaries and restarts installed podswitch user services.
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
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/grandcat/zeroconf"

	"github.com/Ozdotdotdot/podswitch/internal/agent"
	"github.com/Ozdotdotdot/podswitch/internal/config"
	"github.com/Ozdotdotdot/podswitch/internal/coordinator"
	"github.com/Ozdotdotdot/podswitch/internal/discovery"
	"github.com/Ozdotdotdot/podswitch/internal/updater"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "update" {
		runUpdate()
		return
	}
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
		fmt.Fprintln(os.Stderr, "usage: podswitchd -mode agent|coordinator [options] | podswitchd update")
		os.Exit(1)
	}
}

func runUpdate() {
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "podswitchd update: locate executable: %v\n", err)
		os.Exit(1)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	binDir := filepath.Dir(executable)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fmt.Println("Downloading the latest podswitch release...")
	if err := updater.Latest(ctx, binDir); err != nil {
		fmt.Fprintf(os.Stderr, "podswitchd update: %v\n", err)
		os.Exit(1)
	}
	services, err := restartInstalledServices(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "podswitchd update: binaries updated, but restart failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Updated podswitch and podswitchd in %s.\n", binDir)
	if len(services) == 0 {
		fmt.Println("No enabled podswitch user service was found to restart.")
		return
	}
	fmt.Printf("Restarted %s.\n", strings.Join(services, ", "))
}

func restartInstalledServices(ctx context.Context) ([]string, error) {
	units := []string{"podswitch-coordinator.service", "podswitch-agent.service"}
	var restarted []string
	for _, unit := range units {
		enabled := exec.CommandContext(ctx, "systemctl", "--user", "is-enabled", "--quiet", unit).Run() == nil
		active := exec.CommandContext(ctx, "systemctl", "--user", "is-active", "--quiet", unit).Run() == nil
		if !enabled && !active {
			continue
		}
		if err := exec.CommandContext(ctx, "systemctl", "--user", "restart", unit).Run(); err != nil {
			return restarted, fmt.Errorf("restart %s: %w", unit, err)
		}
		restarted = append(restarted, unit)
	}
	return restarted, nil
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
