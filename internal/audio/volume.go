package audio

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type commandRunner func(context.Context, string, ...string) ([]byte, error)

// VolumeDown lowers the active user's default PipeWire sink by five percent.
func VolumeDown(ctx context.Context) error {
	return setVolume(ctx, runCommand, "@DEFAULT_AUDIO_SINK@", "5%-")
}

// VolumeUp raises the active user's default PipeWire sink by five percent,
// without allowing it to exceed 100%.
func VolumeUp(ctx context.Context) error {
	return setVolume(ctx, runCommand, "-l", "1.0", "@DEFAULT_AUDIO_SINK@", "5%+")
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func setVolume(ctx context.Context, runner commandRunner, args ...string) error {
	out, err := runner(ctx, "wpctl", append([]string{"set-volume"}, args...)...)
	if err != nil {
		return fmt.Errorf("wpctl set-volume: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
