// Command podswitch is the CLI for talking to a running podswitchd
// coordinator: "podswitch status" to see who's connected to the AirPods,
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
		cliHostShortcut(os.Args[1:])
		return
	}
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

// cliHostShortcut makes the quick shell forms ergonomic: "podswitch pi"
// grabs the headphones for pi, while "podswitch pi p" toggles pi's MPD.
func cliHostShortcut(args []string) {
	if len(args) < 1 || len(args) > 2 || (len(args) == 2 && args[1] != "p") {
		fmt.Fprintln(os.Stderr, "usage: podswitch <host> [p]")
		os.Exit(1)
	}
	host := args[0]
	action := "grab"
	if len(args) == 2 {
		action = "toggle"
	}
	result, err := hostAction(normalizeHTTP(resolveCoordinatorOrExit("")), host, action)
	if err != nil {
		fmt.Fprintf(os.Stderr, "podswitch %s: %v\n", host, err)
		os.Exit(1)
	}
	if action == "grab" {
		fmt.Printf("AirPods are here (%s).\n", result.Holder)
		return
	}
	if result.Playing != nil && *result.Playing {
		fmt.Printf("playing on %s\n", host)
	} else if result.Playing != nil {
		fmt.Printf("paused on %s\n", host)
	} else {
		fmt.Printf("playback toggled on %s\n", host)
	}
}

type hostActionResult struct {
	Holder  string `json:"holder"`
	Playing *bool  `json:"playing"`
	Error   string `json:"error"`
}

func hostAction(base, host, action string) (hostActionResult, error) {
	path := "/api/grab"
	if action == "toggle" {
		path = "/api/toggle"
	}
	body, _ := json.Marshal(map[string]string{"host": host})
	resp, err := http.Post(base+path, "application/json", strings.NewReader(string(body)))
	if err != nil {
		return hostActionResult{}, err
	}
	defer resp.Body.Close()
	var result hostActionResult
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != http.StatusOK {
		if result.Error == "" {
			result.Error = resp.Status
		}
		return hostActionResult{}, fmt.Errorf("%s", result.Error)
	}
	return result, nil
}

type stateResp struct {
	Holder         string        `json:"holder,omitempty"`
	ConnectedHosts []string      `json:"connectedHosts"`
	AudioOwner     string        `json:"audioOwner,omitempty"`
	ActiveSource   string        `json:"activeSource,omitempty"`
	SourceType     string        `json:"sourceType,omitempty"`
	SourceSeenAt   string        `json:"sourceSeenAt,omitempty"`
	Agents         []agentStatus `json:"agents"`
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
			note = "  (connected)"
		}
		if a.Host == state.AudioOwner {
			note = "  (audio owner)"
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
