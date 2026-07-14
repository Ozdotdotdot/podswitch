package mpd

import "testing"

func TestPlayingOutput(t *testing.T) {
	for output, want := range map[string]bool{
		"song\n[playing] #1/10 0:01/3:00 (0%)\n": true,
		"song\n[paused] #1/10 0:01/3:00 (0%)\n":  false,
		"volume: 50%\n":                          false,
	} {
		if got := playingOutput(output); got != want {
			t.Errorf("playingOutput(%q) = %v, want %v", output, got, want)
		}
	}
}
