// Command podswitch is the CLI for talking to a running podswitchd
// coordinator: "podswitch status" to see who's holding the AirPods,
// "podswitch here" to grab them, "podswitch config" to pin a coordinator
// address. Bare "podswitch" (no subcommand) launches an interactive TUI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Ozdotdotdot/podswitch/internal/config"
	"github.com/Ozdotdotdot/podswitch/internal/tui"
)

func main() {
	if len(os.Args) == 1 {
		cliTUI(nil)
		return
	}
	if os.Args[1] == "tui" {
		cliTUI(os.Args[2:])
		return
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "here":
			cliHere(os.Args[2:])
			return
		case "status":
			cliStatus(os.Args[2:])
			return
		case "config":
			cliConfig(os.Args[2:])
			return
		}
	}
	fmt.Fprintln(os.Stderr, "usage: podswitch [tui] [--coordinator host:port] | here | status | config coordinator <host:port>")
	os.Exit(1)
}

func cliTUI(args []string) {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	coord := fs.String("coordinator", "", "coordinator host:port (overrides env/cache/mDNS)")
	fs.Parse(args)
	if err := tui.Run(*coord); err != nil {
		fmt.Fprintf(os.Stderr, "podswitch: %v\n", err)
		os.Exit(1)
	}
}

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
		fmt.Fprintln(os.Stderr, "usage: podswitch config coordinator <host:port>")
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

	var result struct {
		Status string `json:"status"`
		Holder string `json:"holder"`
		Error  string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "podswitch here: %s\n", result.Error)
		os.Exit(1)
	}
	fmt.Printf("AirPods are here (%s).\n", result.Holder)
}

type stateResp struct {
	Holder string        `json:"holder,omitempty"`
	Agents []agentStatus `json:"agents"`
}

type agentStatus struct {
	Host      string `json:"host"`
	Online    bool   `json:"online"`
	Connected bool   `json:"connected"`
	SeenAt    string `json:"seenAt,omitempty"`
}

func cliStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	coord := fs.String("coordinator", "", "coordinator host:port (overrides env/cache/mDNS)")
	asJSON := fs.Bool("json", false, "print raw JSON instead of a human-readable table")
	fs.Parse(args)

	base := normalizeHTTP(resolveCoordinatorOrExit(*coord))
	resp, err := http.Get(base + "/api/state")
	if err != nil {
		fmt.Fprintf(os.Stderr, "podswitch status: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if *asJSON {
		out, _ := io.ReadAll(resp.Body)
		fmt.Println(string(out))
		return
	}

	var state stateResp
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		fmt.Fprintf(os.Stderr, "podswitch status: %v\n", err)
		os.Exit(1)
	}
	printStatus(state)
}

func printStatus(state stateResp) {
	if len(state.Agents) == 0 {
		fmt.Println("no agents connected")
		return
	}
	for _, a := range state.Agents {
		marker := "○"
		note := ""
		if a.Connected {
			marker = "●"
			note = "  (holding the AirPods)"
		}
		age := ""
		if seen, err := time.Parse(time.RFC3339, a.SeenAt); err == nil {
			age = fmt.Sprintf("  last seen %s ago", time.Since(seen).Round(time.Second))
		}
		fmt.Printf("%s %-16s%s%s\n", marker, a.Host, note, age)
	}
}

func normalizeHTTP(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}
