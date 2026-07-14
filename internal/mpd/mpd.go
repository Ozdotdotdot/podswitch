// Package mpd provides the deliberately small slice of MPD control used by
// podswitch: control playback and watch its player state. It shells out to
// the standard mpc client so it respects the user's normal MPD configuration.
package mpd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const retryDelay = 15 * time.Second

// Playing returns whether MPD currently reports playback in progress.
func Playing(ctx context.Context) (bool, error) {
	out, err := exec.CommandContext(ctx, "mpc", "status").Output()
	if err != nil {
		return false, fmt.Errorf("mpc status: %w", err)
	}
	return playingOutput(string(out)), nil
}

func playingOutput(output string) bool { return strings.Contains(output, "[playing]") }

// Toggle asks MPD to switch between play and pause.
func Toggle(ctx context.Context) error {
	return run(ctx, "toggle")
}

// Previous skips to the previous MPD track.
func Previous(ctx context.Context) error { return run(ctx, "prev") }

// Next skips to the next MPD track.
func Next(ctx context.Context) error { return run(ctx, "next") }

func run(ctx context.Context, action string) error {
	if out, err := exec.CommandContext(ctx, "mpc", action).CombinedOutput(); err != nil {
		return fmt.Errorf("mpc %s: %w: %s", action, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Watch reports the current state, then waits for MPD's event-driven "idle
// player" notification before reading the state again. It intentionally does
// not poll. If MPD is temporarily unavailable it retries its event stream
// after a modest delay, so an agent can start before MPD does.
func Watch(ctx context.Context, report func(bool)) {
	for {
		if ctx.Err() != nil {
			return
		}
		if playing, err := Playing(ctx); err == nil {
			report(playing)
		}

		cmd := exec.CommandContext(ctx, "mpc", "idle", "player")
		if err := cmd.Run(); ctx.Err() != nil {
			return
		} else if err != nil {
			if !wait(ctx, retryDelay) {
				return
			}
			continue
		}

		if playing, err := Playing(ctx); err == nil {
			report(playing)
		} else if !wait(ctx, retryDelay) {
			return
		}
	}
}

func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
