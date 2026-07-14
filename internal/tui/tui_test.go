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
	if m.agents[0].Host != "laptop" || !strings.Contains(m.hostList(), "holding") {
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
