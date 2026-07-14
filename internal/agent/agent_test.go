package agent

import (
	"context"
	"testing"

	"github.com/Ozdotdotdot/podswitch/internal/proto"
)

func TestDoMediaDispatchesOnlySupportedActions(t *testing.T) {
	var called string
	a := &Agent{media: mediaHandlers{volumeDown: func(context.Context) error { called = "down"; return nil }, volumeUp: func(context.Context) error { called = "up"; return nil }, previous: func(context.Context) error { called = "previous"; return nil }, next: func(context.Context) error { called = "next"; return nil }}}
	for action, want := range map[string]string{proto.ActionVolumeDown: "down", proto.ActionVolumeUp: "up", proto.ActionPrevious: "previous", proto.ActionNext: "next"} {
		called = ""
		if err := a.doMedia(context.Background(), action); err != nil {
			t.Fatalf("doMedia(%q): %v", action, err)
		}
		if called != want {
			t.Errorf("doMedia(%q) called %q, want %q", action, called, want)
		}
	}
	if err := a.doMedia(context.Background(), "shell"); err == nil {
		t.Fatal("unsupported action was accepted")
	}
}
