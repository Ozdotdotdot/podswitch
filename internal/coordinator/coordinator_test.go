package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/Ozdotdotdot/podswitch/internal/proto"
)

func TestCurrentStateRepresentsMultipointWithoutGuessingOwner(t *testing.T) {
	c := New()
	c.agents["workstation"] = &agentConn{host: "workstation", connected: true}
	c.agents["pi"] = &agentConn{host: "pi", connected: true}

	state := c.currentState()
	if state.Holder != "" || state.AudioOwner != "" {
		t.Fatalf("ambiguous ownership was guessed: %#v", state)
	}
	if want := []string{"pi", "workstation"}; !reflect.DeepEqual(state.ConnectedHosts, want) {
		t.Fatalf("connected hosts = %v, want %v", state.ConnectedHosts, want)
	}
	if state.Agents[0].Host != "pi" || state.Agents[1].Host != "workstation" {
		t.Fatalf("agents are not deterministic: %#v", state.Agents)
	}
}

func TestCurrentStateRetainsLegacyHolderForSingleConnection(t *testing.T) {
	c := New()
	c.agents["pi"] = &agentConn{host: "pi", connected: true}

	state := c.currentState()
	if state.Holder != "pi" {
		t.Fatalf("holder = %q, want pi", state.Holder)
	}
}

func TestToggleForwardsCommandAndReturnsPlaybackState(t *testing.T) {
	c := New()
	server := httptest.NewServer(c.Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent"
	conn, _, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := wsjson.Write(ctx, conn, proto.Envelope{Type: proto.TypeHello, Host: "laptop"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for len(c.currentState().Agents) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(c.currentState().Agents) == 0 {
		t.Fatal("agent did not register")
	}
	watchConn, _, err := websocket.Dial(ctx, endpoint[:len(endpoint)-len("/agent")]+"/watch", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer watchConn.CloseNow()
	var initial stateResp
	if err := wsjson.Read(ctx, watchConn, &initial); err != nil {
		t.Fatal(err)
	}
	if len(initial.Agents) != 1 || initial.Agents[0].Playing {
		t.Fatalf("unexpected initial watcher state: %#v", initial)
	}

	done := make(chan error, 1)
	go func() {
		var command proto.Envelope
		if err := wsjson.Read(ctx, conn, &command); err != nil {
			done <- err
			return
		}
		if command.Action != proto.ActionToggle {
			done <- errUnexpectedAction(command.Action)
			return
		}
		playing := true
		if err := wsjson.Write(ctx, conn, proto.Envelope{Type: proto.TypeState, Playing: &playing}); err != nil {
			done <- err
			return
		}
		done <- wsjson.Write(ctx, conn, proto.Envelope{Type: proto.TypeResult, ReqID: command.ReqID, OK: true, Playing: &playing})
	}()

	resp, err := http.Post(server.URL+"/api/toggle", "application/json", bytes.NewBufferString(`{"host":"laptop"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %s", resp.Status)
	}
	var body struct {
		Host    string `json:"host"`
		Playing *bool  `json:"playing"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Host != "laptop" || body.Playing == nil || !*body.Playing {
		t.Fatalf("unexpected response: %#v", body)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var pushed stateResp
	if err := wsjson.Read(ctx, watchConn, &pushed); err != nil {
		t.Fatal(err)
	}
	if len(pushed.Agents) != 1 || !pushed.Agents[0].Playing {
		t.Fatalf("watcher did not receive playback update: %#v", pushed)
	}
	state := c.currentState()
	if !state.Agents[0].Playing {
		t.Fatalf("playback state was not retained: %#v", state)
	}
}

type unexpectedAction string

func (e unexpectedAction) Error() string { return "unexpected action: " + string(e) }

func errUnexpectedAction(action string) error { return unexpectedAction(action) }
