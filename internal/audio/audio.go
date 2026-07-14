// Package audio ports the proven AirpodsOn.sh / AirpodsOff.sh PipeWire
// routing logic into Go: activate A2DP, park streams on a transition null
// sink while the card profile flips, then move everything onto (or off of)
// the AirPods sink. See DESIGN.md for why the null-sink parking exists
// (avoids apps latching onto a sink that's about to die mid-transition).
package audio

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const transitionSink = "airpods_transition"

// RouteTo activates the AirPods' A2DP profile and moves all audio onto it.
// cardName/sinkPrefix are config.PipeWireCard/PipeWireSinkPrefix.
func RouteTo(ctx context.Context, cardName, sinkPrefix string) error {
	cleanupStaleTransitionModules(ctx)

	nullModule, err := parkStreamsOnTransitionSink(ctx)
	if err != nil {
		return fmt.Errorf("park streams: %w", err)
	}
	defer unloadModule(ctx, nullModule)

	if err := activateA2DP(ctx, cardName); err != nil {
		return fmt.Errorf("activate a2dp: %w", err)
	}

	sink, err := waitForSink(ctx, sinkPrefix, 10*time.Second)
	if err != nil {
		return fmt.Errorf("wait for airpods sink: %w", err)
	}

	if err := run(ctx, "pactl", "set-default-sink", sink); err != nil {
		return fmt.Errorf("set default sink: %w", err)
	}
	moveAllStreamsTo(ctx, sink)
	return nil
}

// RouteAway parks streams onto the fallback sink and deactivates the
// AirPods' A2DP profile (BT link is left up for higher layers to manage).
func RouteAway(ctx context.Context, cardName, fallbackSink string) error {
	nullModule, err := parkStreamsOnTransitionSink(ctx)
	if err != nil {
		return fmt.Errorf("park streams: %w", err)
	}
	defer unloadModule(ctx, nullModule)

	if err := run(ctx, "pactl", "set-default-sink", fallbackSink); err != nil {
		return fmt.Errorf("set default sink: %w", err)
	}
	moveAllStreamsTo(ctx, fallbackSink)

	// Best-effort: deactivating the profile can race with WirePlumber; the
	// streams are already settled on the fallback sink by this point.
	_ = run(ctx, "pactl", "set-card-profile", cardName, "off")
	return nil
}

// FallbackSink returns the first non-AirPods, non-transition sink — the
// same dynamic-resolution pattern as the Pi's AirpodsOff.sh copy, so this
// works across hosts without a hardcoded sink name.
func FallbackSink(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "pactl", "list", "sinks", "short").Output()
	if err != nil {
		return "", fmt.Errorf("pactl list sinks: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[1]
		if strings.HasPrefix(name, "bluez") || name == transitionSink {
			continue
		}
		return name, nil
	}
	return "", fmt.Errorf("no non-bluez fallback sink found")
}

func cleanupStaleTransitionModules(ctx context.Context) {
	out, err := exec.CommandContext(ctx, "pactl", "list", "modules", "short").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "module-null-sink") || !strings.Contains(line, transitionSink) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			unloadModule(ctx, fields[0])
		}
	}
}

// parkStreamsOnTransitionSink creates the null sink, sets it default (so
// anything reconnecting mid-transition lands here, not on a dying sink),
// then moves every existing stream onto it. Returns the module id to unload
// once the real sink is ready.
func parkStreamsOnTransitionSink(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "pactl", "load-module", "module-null-sink",
		"sink_name="+transitionSink, "sink_properties=device.description=AirPods_Transition").Output()
	if err != nil {
		return "", err
	}
	moduleID := strings.TrimSpace(string(out))

	if err := run(ctx, "pactl", "set-default-sink", transitionSink); err != nil {
		return moduleID, err
	}
	moveAllStreamsTo(ctx, transitionSink)
	return moduleID, nil
}

func moveAllStreamsTo(ctx context.Context, sink string) {
	out, err := exec.CommandContext(ctx, "pactl", "list", "sink-inputs", "short").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		_ = run(ctx, "pactl", "move-sink-input", fields[0], sink)
	}
}

func activateA2DP(ctx context.Context, cardName string) error {
	profiles := []string{"a2dp-sink-sbc_xq", "a2dp-sink-sbc", "a2dp-sink"}
	var lastErr error
	for _, p := range profiles {
		if err := run(ctx, "pactl", "set-card-profile", cardName, p); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

// waitForSink polls for a sink matching prefix — PipeWire's suffix after
// the MAC isn't stable, so always prefix-match. bluetoothctl/BlueZ's
// Connect returns before PipeWire registers the card; this is the "wait on
// the resource, never a fixed sleep" fix from the POC (250ms poll, capped).
func waitForSink(ctx context.Context, prefix string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.CommandContext(ctx, "pactl", "list", "sinks", "short").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 2 {
					continue
				}
				if strings.HasPrefix(fields[1], prefix) {
					return fields[1], nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("sink with prefix %q did not appear within %s", prefix, timeout)
}

// WaitForCard polls for the PipeWire card itself (not yet the sink) —
// mirrors AirpodsHere.sh's wait_for_card, used right after a BlueZ Connect
// before attempting any profile/routing calls.
func WaitForCard(ctx context.Context, cardName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.CommandContext(ctx, "pactl", "list", "cards", "short").Output()
		if err == nil && strings.Contains(string(out), cardName) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("card %q did not appear within %s", cardName, timeout)
}

func unloadModule(ctx context.Context, moduleID string) {
	if moduleID == "" {
		return
	}
	_ = run(ctx, "pactl", "unload-module", moduleID)
}

func run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}
