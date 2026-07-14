package audio

import (
	"context"
	"testing"
)

func TestSetVolumeUsesWPCTLArguments(t *testing.T) {
	for _, test := range []struct {
		name       string
		args, want []string
	}{
		{name: "down", args: []string{"@DEFAULT_AUDIO_SINK@", "5%-"}, want: []string{"set-volume", "@DEFAULT_AUDIO_SINK@", "5%-"}},
		{name: "up capped at one", args: []string{"-l", "1.0", "@DEFAULT_AUDIO_SINK@", "5%+"}, want: []string{"set-volume", "-l", "1.0", "@DEFAULT_AUDIO_SINK@", "5%+"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var gotName string
			var gotArgs []string
			runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
				gotName = name
				gotArgs = append([]string(nil), args...)
				return nil, nil
			}
			if err := setVolume(context.Background(), runner, test.args...); err != nil {
				t.Fatal(err)
			}
			if gotName != "wpctl" || !equalStrings(gotArgs, test.want) {
				t.Fatalf("command = %q %q, want wpctl %q", gotName, gotArgs, test.want)
			}
		})
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
