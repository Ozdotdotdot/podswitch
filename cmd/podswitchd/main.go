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
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Ozdotdotdot/podswitch/internal/agent"
	"github.com/Ozdotdotdot/podswitch/internal/config"
	"github.com/Ozdotdotdot/podswitch/internal/coordinator"
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
		}
	}
	runDaemon()
}

func runDaemon() {
	mode := flag.String("mode", "auto", "role: auto|agent|coordinator")
	addr := flag.String("addr", config.DefaultCoordinatorAddr, "coordinator: listen address")
	coordinatorAddr := flag.String("coordinator", "", "agent: coordinator host:port (or ws(s):// URL)")
	host := flag.String("host", config.Hostname(), "agent: identity reported to the coordinator")
	flag.Parse()

	resolved := *mode
	if resolved == "auto" {
		if *coordinatorAddr != "" {
			resolved = "agent"
		} else {
			resolved = "coordinator"
		}
	}

	switch resolved {
	case "coordinator":
		runCoordinator(*addr)
	case "agent":
		if *coordinatorAddr == "" {
			log.Fatal("agent mode requires -coordinator <host:port>")
		}
		runAgent(*host, *coordinatorAddr)
	default:
		log.Fatalf("unknown mode %q", resolved)
	}
}

func runCoordinator(addr string) {
	c := coordinator.New()
	srv := &http.Server{Addr: addr, Handler: c.Handler()}

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

func coordinatorBaseURL(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("PODSWITCH_COORDINATOR"); v != "" {
		return v
	}
	log.Fatal("no coordinator address: pass -coordinator or set PODSWITCH_COORDINATOR (host:port)")
	return ""
}

func cliHere(args []string) {
	fs := flag.NewFlagSet("here", flag.ExitOnError)
	coord := fs.String("coordinator", "", "coordinator host:port")
	host := fs.String("host", config.Hostname(), "host to grab the AirPods onto")
	fs.Parse(args)

	base := normalizeHTTP(coordinatorBaseURL(*coord))
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
	coord := fs.String("coordinator", "", "coordinator host:port")
	fs.Parse(args)

	base := normalizeHTTP(coordinatorBaseURL(*coord))
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
