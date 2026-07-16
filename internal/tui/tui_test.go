package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStateSelectionAndGrab(t *testing.T) {
	var requestedHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/grab" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		defer r.Body.Close()
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		requestedHost = payload["host"]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	m := newModel(server.URL)
	updated, _ := m.Update(stateMsg{Agents: []agentStatus{
		{Host: "studio", Online: true, Connected: true},
		{Host: "laptop", Online: true},
	}})
	m = updated.(model)
	if m.agents[0].Host != "laptop" || !strings.Contains(m.hostList(), "connected") {
		t.Fatalf("state was not sorted/rendered correctly: %#v\n%s", m.agents, m.hostList())
	}
	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if !m.grabbing || command == nil {
		t.Fatal("enter on an available host did not start a grab")
	}
	updated, _ = m.Update(command())
	m = updated.(model)
	if m.grabbing {
		t.Fatal("grab did not finish")
	}
	if requestedHost != "laptop" {
		t.Fatalf("grab targeted %q, want laptop", requestedHost)
	}
}

func TestHostListDistinguishesConnectionFromAudioOwnership(t *testing.T) {
	m := newModel("http://coordinator")
	updated, _ := m.Update(stateMsg{
		AudioOwner: "pi",
		SourceType: "none",
		Agents: []agentStatus{
			{Host: "pi", Online: true, Connected: true},
			{Host: "workstation", Online: true, Connected: true},
		},
	})
	m = updated.(model)
	rendered := m.hostList()
	if !strings.Contains(rendered, "audio owner · idle") || !strings.Contains(rendered, "connected") {
		t.Fatalf("ownership and connection were not distinguished:\n%s", rendered)
	}
}

func TestWebSocketURL(t *testing.T) {
	for input, want := range map[string]string{
		"server:8090":           "ws://server:8090/ws/watch",
		"https://server:8090/x": "wss://server:8090/ws/watch",
	} {
		if got := normalizeWS(input); got != want {
			t.Errorf("normalizeWS(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHandoffCompletesWhenWatchStateArrivesBeforeHTTPResult(t *testing.T) {
	m := newModel("http://example.invalid")
	m.grabbing = true
	m.grabTarget = "laptop"

	updated, _ := m.Update(stateMsg{Agents: []agentStatus{{Host: "laptop", Online: true, Connected: true}}})
	m = updated.(model)
	if !m.grabConnected {
		t.Fatal("watch state did not confirm the selected target")
	}
	updated, _ = m.Update(grabResultMsg{})
	m = updated.(model)
	if m.message != "handoff complete" {
		t.Fatalf("message = %q, want handoff complete", m.message)
	}
}

func TestFailedGrabCannotBeReportedAsComplete(t *testing.T) {
	m := newModel("http://example.invalid")
	m.grabbing = true
	m.grabTarget = "pi"

	// BlueZ can report Connected before PipeWire is actually ready.
	updated, _ := m.Update(stateMsg{Agents: []agentStatus{{Host: "pi", Online: true, Connected: true}}})
	m = updated.(model)
	updated, _ = m.Update(grabResultMsg{err: errTest("wait for card: deadline exceeded")})
	m = updated.(model)
	if m.message == "handoff complete" || !strings.Contains(m.message, "grab failed:") {
		t.Fatalf("failed handoff was reported as %q", m.message)
	}
	if m.grabTarget != "" || m.grabConnected || m.grabSucceeded {
		t.Fatalf("failed attempt retained success state: %#v", m)
	}
}

func TestTogglePlaybackAndIndicator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/toggle" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		defer r.Body.Close()
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["host"] != "laptop" {
			t.Fatalf("toggle host = %q", payload["host"])
		}
		_, _ = w.Write([]byte(`{"playing":true}`))
	}))
	defer server.Close()

	m := newModel(server.URL)
	updated, _ := m.Update(stateMsg{Agents: []agentStatus{{Host: "laptop", Online: true, Playing: true}}})
	m = updated.(model)
	m.message = "live updates connected"
	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(model)
	if !m.toggling || command == nil {
		t.Fatal("p did not start a playback toggle")
	}
	if m.message != "live updates connected" {
		t.Fatalf("toggle added transient status noise: %q", m.message)
	}
	updated, _ = m.Update(command())
	m = updated.(model)
	if m.toggling || m.message != "playing on laptop" {
		t.Fatalf("toggle result = toggling:%v message:%q", m.toggling, m.message)
	}
	if !strings.Contains(m.hostList(), "♪") {
		t.Fatalf("playing host is missing music indicator: %s", m.hostList())
	}
	if strings.Index(m.hostList(), "online") > strings.Index(m.hostList(), "♪") {
		t.Fatalf("music indicator appeared before state text: %s", m.hostList())
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestAnimationDimensions(t *testing.T) {
	lines := strings.Split(renderHeadset(0), "\n")
	if len(lines) != artHeight {
		t.Fatalf("height = %d, want %d", len(lines), artHeight)
	}
	for _, line := range lines {
		if len(line) != artWidth {
			t.Fatalf("width = %d, want %d", len(line), artWidth)
		}
	}
}
