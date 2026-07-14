package tui

import "testing"

func TestMediaKeyMappingAndLabels(t *testing.T) {
	for key, action := range map[string]string{"[": "volume-down", "]": "volume-up", "<": "previous", ">": "next"} {
		if got := mediaActionForKey(key); got != action {
			t.Errorf("%q mapped to %q, want %q", key, got, action)
		}
		if mediaSuccess(action) == "media action complete" {
			t.Errorf("%q has no success label", action)
		}
	}
	if got := mediaActionForKey("x"); got != "" {
		t.Errorf("unknown key mapped to %q", got)
	}
}
