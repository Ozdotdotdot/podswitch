package aacp

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestParseSource(t *testing.T) {
	at := time.Unix(123, 0)
	packet := mustHex("040004000e0077deabeb27b802")
	event, ok := ParseSource(packet, at)
	if !ok {
		t.Fatal("source packet was not recognized")
	}
	if event.MAC != "B8:27:EB:AB:DE:77" || event.Type != "media" || event.At != at {
		t.Fatalf("unexpected event: %#v", event)
	}
}

func TestLiveObserve(t *testing.T) {
	mac := os.Getenv("PODSWITCH_AACP_LIVE_MAC")
	if mac == "" {
		t.Skip("set PODSWITCH_AACP_LIVE_MAC to run against connected AirPods")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var event SourceEvent
	err := Observe(ctx, mac, func(received SourceEvent) {
		event = received
		cancel()
	})
	if err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
	if event.MAC == "" {
		t.Fatal("no AUDIO_SOURCE notification received")
	}
	t.Logf("source=%s type=%s", event.MAC, event.Type)
}

func TestParseSourceRejectsOtherPackets(t *testing.T) {
	if _, ok := ParseSource(mustHex("0400040009000601000000"), time.Now()); ok {
		t.Fatal("control packet was parsed as an audio source")
	}
}
