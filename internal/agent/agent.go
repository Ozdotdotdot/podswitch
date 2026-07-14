// Package agent runs on each audio-endpoint host (laptop/Pi/workstation).
// It dials out to the coordinator over a persistent WebSocket (so a
// sleeping/roaming host never needs to be addressed inbound), watches local
// BlueZ state event-driven and pushes changes up, and executes
// connect/disconnect commands pushed down by actuating BlueZ + routing
// PipeWire audio.
package agent

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/Ozdotdotdot/podswitch/internal/audio"
	"github.com/Ozdotdotdot/podswitch/internal/bluez"
	"github.com/Ozdotdotdot/podswitch/internal/config"
	"github.com/Ozdotdotdot/podswitch/internal/proto"
)

const (
	dialTimeout    = 10 * time.Second
	reconnectDelay = 5 * time.Second
	// A freshly connected Bluetooth card can take longer than the initial
	// card appearance to expose an A2DP profile, especially on low-power
	// hosts. Leave enough time for the bounded PipeWire readiness checks.
	commandTimeout = 40 * time.Second
	version        = "0.1.0"
)

// Agent connects to a coordinator and drives the local AirPods.
type Agent struct {
	Host           string
	CoordinatorURL string // e.g. ws://switchserver:9090/ws/agent

	bt      *bluez.Watcher
	headset config.Headset
}

// New prepares an agent bound to the given coordinator WS URL.
func New(host, coordinatorURL string) (*Agent, error) {
	headset, err := config.CurrentHeadset()
	if err != nil {
		return nil, err
	}
	bt, err := bluez.New(headset.DevicePath)
	if err != nil {
		return nil, err
	}
	return &Agent{Host: host, CoordinatorURL: coordinatorURL, bt: bt, headset: headset}, nil
}

// Run connects and reconnects forever until ctx is cancelled.
func (a *Agent) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := a.runOnce(ctx); err != nil {
			log.Printf("agent: connection to coordinator lost: %v (retrying in %s)", err, reconnectDelay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}

func (a *Agent) runOnce(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	conn, _, err := websocket.Dial(dialCtx, a.CoordinatorURL, nil)
	cancel()
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	if err := wsjson.Write(ctx, conn, proto.Envelope{Type: proto.TypeHello, Host: a.Host, Version: version}); err != nil {
		return err
	}
	log.Printf("agent: connected to coordinator as %q", a.Host)

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Push the current state immediately, then on every BlueZ change.
	go a.watchAndReport(connCtx, conn)

	for {
		var msg proto.Envelope
		if err := wsjson.Read(connCtx, conn, &msg); err != nil {
			return err
		}
		if msg.Type != proto.TypeCommand {
			continue
		}
		go a.handleCommand(connCtx, conn, msg)
	}
}

func (a *Agent) watchAndReport(ctx context.Context, conn *websocket.Conn) {
	if connected, err := a.bt.Connected(); err == nil {
		a.reportState(ctx, conn, connected)
	}
	err := a.bt.Watch(ctx, func(connected bool) {
		a.reportState(ctx, conn, connected)
	})
	if err != nil && ctx.Err() == nil {
		log.Printf("agent: bluez watch ended: %v", err)
	}
}

func (a *Agent) reportState(ctx context.Context, conn *websocket.Conn, connected bool) {
	c := connected
	if err := wsjson.Write(ctx, conn, proto.Envelope{Type: proto.TypeState, Connected: &c}); err != nil {
		log.Printf("agent: report state: %v", err)
	}
}

func (a *Agent) handleCommand(ctx context.Context, conn *websocket.Conn, cmd proto.Envelope) {
	cmdCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	var err error
	switch cmd.Action {
	case proto.ActionDisconnect:
		err = a.doDisconnect(cmdCtx)
	case proto.ActionConnect:
		err = a.doConnect(cmdCtx)
	default:
		err = errUnknownAction(cmd.Action)
	}

	res := proto.Envelope{Type: proto.TypeResult, ReqID: cmd.ReqID, OK: err == nil}
	if err != nil {
		res.Error = err.Error()
		log.Printf("agent: command %s failed: %v", cmd.Action, err)
	} else {
		log.Printf("agent: command %s ok", cmd.Action)
	}
	if werr := wsjson.Write(ctx, conn, res); werr != nil {
		log.Printf("agent: send result: %v", werr)
	}
}

func (a *Agent) doDisconnect(ctx context.Context) error {
	if err := a.bt.Disconnect(ctx); err != nil {
		return err
	}
	fallback, err := audio.FallbackSink(ctx)
	if err != nil {
		return err
	}
	return audio.RouteAway(ctx, a.headset.PipeWireCard, fallback)
}

func (a *Agent) doConnect(ctx context.Context) error {
	connected, err := a.bt.Connected()
	if err != nil {
		return err
	}
	if !connected {
		if err := a.bt.Connect(ctx); err != nil {
			return err
		}
	}
	if err := audio.WaitForCard(ctx, a.headset.PipeWireCard, 10*time.Second); err != nil {
		return err
	}
	return audio.RouteTo(ctx, a.headset.PipeWireCard, a.headset.PipeWireSinkPrefix)
}

func errUnknownAction(action string) error {
	return fmt.Errorf("unknown command action %q", action)
}

// BuildCoordinatorURL turns a bare host:port (or ws(s):// URL) into the
// agent WS endpoint URL.
func BuildCoordinatorURL(addr string) string {
	if u, err := url.Parse(addr); err == nil && (u.Scheme == "ws" || u.Scheme == "wss") {
		return addr
	}
	return "ws://" + addr + "/ws/agent"
}
