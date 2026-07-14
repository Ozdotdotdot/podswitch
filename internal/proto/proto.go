// Package proto defines the WebSocket message envelope exchanged between an
// agent (dials out) and the coordinator (accepts). Agents push hello/state
// messages up; the coordinator pushes command messages down and agents
// reply with results.
package proto

// Type values for Envelope.Type.
const (
	TypeHello   = "hello"
	TypeState   = "state"
	TypeCommand = "command"
	TypeResult  = "result"
)

// Command actions carried in a "command" envelope.
const (
	ActionDisconnect = "disconnect"
	ActionConnect    = "connect" // connect + route audio locally
	ActionToggle     = "toggle"  // toggle local MPD playback
)

// Envelope is the single message shape on the wire; only the fields
// relevant to Type are populated.
type Envelope struct {
	Type string `json:"type"`

	// hello (agent -> coordinator)
	Host    string `json:"host,omitempty"`
	Version string `json:"version,omitempty"`

	// state (agent -> coordinator): live BlueZ Connected property for the
	// AirPods device on this host, and optional local MPD playback state.
	Connected *bool `json:"connected,omitempty"`
	Playing   *bool `json:"playing,omitempty"`

	// command (coordinator -> agent)
	ReqID  string `json:"reqId,omitempty"`
	Action string `json:"action,omitempty"`

	// result (agent -> coordinator)
	OK    bool   `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
}
