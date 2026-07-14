// Command podswitchd is the single podswitch binary: it runs as the
// coordinator (always-on switch server) or as an agent (audio-endpoint
// host), and doubles as the CLI ("podswitchd here" / "podswitchd state")
// for talking to a running coordinator.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "here":
			cliHere(os.Args[2:])
			return
		case "state":
			cliState(os.Args[2:])
			return
		case "config":
			cliConfig(os.Args[2:])
			return
		}
	}
	runDaemon()
}

func runDaemon() {
	mode := flag.String("mode", "auto", "role: auto|agent|coordinator")
	addr := flag.String("addr", config.DefaultCoordinatorAddr, "coordinator: listen address")
	coordinatorAddr := flag.String("coordinator", "", "agent: coordinator host:port (or ws(s):// URL); resolved via env/cache/mDNS if unset")
	host := flag.String("host", config.Hostname(), "agent: identity reported to the coordinator")
	flag.Parse()

	resolved := *mode
	if resolved == "auto" {
		if *coordinatorAddr != "" || os.Getenv("PODSWITCH_COORDINATOR") != "" {
			resolved = "agent"
		} else {
			resolved = "coordinator"
		}
	}

	switch resolved {
	case "coordinator":
		runCoordinator(*addr)
	case "agent":
		coord, err := config.ResolveCoordinatorAddr(*coordinatorAddr)
		if err != nil {
			log.Fatalf("podswitchd: %v", err)
		}
		runAgent(*host, coord)
	default:
		log.Fatalf("unknown mode %q", resolved)
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

// --- CLI ---

func resolveCoordinatorOrExit(explicit string) string {
	addr, err := config.ResolveCoordinatorAddr(explicit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "podswitch: %v\n", err)
		os.Exit(1)
	}
	return addr
}

func cliConfig(args []string) {
	if len(args) < 2 || args[0] != "coordinator" {
		fmt.Fprintln(os.Stderr, "usage: podswitchd config coordinator <host:port>")
		os.Exit(1)
	}
	addr := args[1]
	if err := config.SetCoordinatorAddr(addr); err != nil {
		fmt.Fprintf(os.Stderr, "podswitch config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("coordinator set to %s (%s)\n", addr, config.UserConfigPath())
}

func cliHere(args []string) {
	fs := flag.NewFlagSet("here", flag.ExitOnError)
	coord := fs.String("coordinator", "", "coordinator host:port (overrides env/cache/mDNS)")
	host := fs.String("host", config.Hostname(), "host to grab the AirPods onto")
	fs.Parse(args)

	base := normalizeHTTP(resolveCoordinatorOrExit(*coord))
	body, _ := json.Marshal(map[string]string{"host": *host})
	resp, err := http.Post(base+"/api/grab", "application/json", strings.NewReader(string(body)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "podswitch here: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	fmt.Println(string(out))
	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}

func cliState(args []string) {
	fs := flag.NewFlagSet("state", flag.ExitOnError)
	coord := fs.String("coordinator", "", "coordinator host:port (overrides env/cache/mDNS)")
	fs.Parse(args)

	base := normalizeHTTP(resolveCoordinatorOrExit(*coord))
	resp, err := http.Get(base + "/api/state")
	if err != nil {
		fmt.Fprintf(os.Stderr, "podswitch state: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	fmt.Println(string(out))
}

func normalizeHTTP(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}
