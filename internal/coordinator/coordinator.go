// Package coordinator runs on the always-on switch server. Agents dial in
// over a persistent WebSocket; the coordinator keeps a cache of who's
// online and connected to the AirPods, and orchestrates handoffs (evict
// connected peers, connect target) on grab requests.
//
// The registry is a cache for widgets, NOT authoritative — Grab re-verifies
// against each agent's live state before acting, and liveness is the
// WebSocket itself (a dropped socket means the host is offline and can't be
// holding the buds; no stale "who has them").
package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/Ozdotdotdot/podswitch/internal/proto"
)

const (
	pingInterval = 20 * time.Second
	// A grab can require an agent disconnect followed by a destination
	// connect. Each agent has its own 60 second bounded readiness operation,
	// so this enclosing deadline must cover both commands without reporting a
	// false failure while PipeWire is still registering the destination sink.
	cmdTimeout = 130 * time.Second
)

// agentConn is one connected agent.
type agentConn struct {
	host          string
	conn          *websocket.Conn
	connected     bool // last known BlueZ Connected state on this host
	playing       bool // last known MPD playback state on this host
	controllerMAC string
	seenAt        time.Time

	mu      sync.Mutex
	pending map[string]chan proto.Envelope // reqID -> result channel
}

func (a *agentConn) send(ctx context.Context, env proto.Envelope) error {
	return wsjson.Write(ctx, a.conn, env)
}

// Coordinator holds the live agent registry and serves the HTTP+WS API.
type Coordinator struct {
	mu           sync.Mutex
	agents       map[string]*agentConn    // host -> conn
	watchers     map[*websocket.Conn]bool // read-only state subscribers (TUI/widgets)
	activeSource string
	sourceType   string
	sourceSeenAt time.Time
}

// New creates an empty Coordinator.
func New() *Coordinator {
	return &Coordinator{
		agents:   make(map[string]*agentConn),
		watchers: make(map[*websocket.Conn]bool),
	}
}

// Handler returns the HTTP mux: WS accept endpoints + curl-able JSON API.
func (c *Coordinator) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/agent", c.handleAgentWS)
	mux.HandleFunc("GET /ws/watch", c.handleWatchWS)
	mux.HandleFunc("GET /api/state", c.handleState)
	mux.HandleFunc("POST /api/grab", c.handleGrab)
	mux.HandleFunc("POST /api/toggle", c.handleToggle)
	mux.HandleFunc("POST /api/media", c.handleMedia)
	return mux
}

type mediaReq struct {
	Host   string `json:"host"`
	Action string `json:"action"`
}

func (c *Coordinator) handleMedia(w http.ResponseWriter, r *http.Request) {
	var req mediaReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Host == "" || !isMediaAction(req.Action) {
		writeErr(w, http.StatusBadRequest, "invalid media action or missing host")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if _, err := c.Media(ctx, req.Host, req.Action); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"host": req.Host, "action": req.Action})
}

type stateResp struct {
	Holder         string        `json:"holder,omitempty"` // legacy: populated only for one connected host
	ConnectedHosts []string      `json:"connectedHosts"`
	AudioOwner     string        `json:"audioOwner,omitempty"` // empty until ownership is directly observed
	ActiveSource   string        `json:"activeSource,omitempty"`
	SourceType     string        `json:"sourceType,omitempty"`
	SourceSeenAt   string        `json:"sourceSeenAt,omitempty"`
	Agents         []agentStatus `json:"agents"`
}

type agentStatus struct {
	Host          string `json:"host"`
	Online        bool   `json:"online"`
	Connected     bool   `json:"connected"`
	Playing       bool   `json:"playing"`
	SeenAt        string `json:"seenAt,omitempty"`
	ControllerMAC string `json:"controllerMac,omitempty"`
}

func (c *Coordinator) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, c.currentState())
}

// currentState snapshots the registry under lock.
func (c *Coordinator) currentState() stateResp {
	c.mu.Lock()
	defer c.mu.Unlock()

	resp := stateResp{ConnectedHosts: []string{}, ActiveSource: c.activeSource, SourceType: c.sourceType}
	if !c.sourceSeenAt.IsZero() {
		resp.SourceSeenAt = c.sourceSeenAt.Format(time.RFC3339Nano)
	}
	for host, a := range c.agents {
		resp.Agents = append(resp.Agents, agentStatus{
			Host:          host,
			Online:        true,
			Connected:     a.connected,
			Playing:       a.playing,
			SeenAt:        a.seenAt.Format(time.RFC3339),
			ControllerMAC: a.controllerMAC,
		})
		if a.connected {
			resp.ConnectedHosts = append(resp.ConnectedHosts, host)
		}
	}
	if resp.SourceType == "media" || resp.SourceType == "call" {
		for host, a := range c.agents {
			if a.connected && strings.EqualFold(a.controllerMAC, resp.ActiveSource) {
				resp.AudioOwner = host
				break
			}
		}
	}
	sort.Slice(resp.Agents, func(i, j int) bool { return resp.Agents[i].Host < resp.Agents[j].Host })
	sort.Strings(resp.ConnectedHosts)
	if len(resp.ConnectedHosts) == 1 {
		resp.Holder = resp.ConnectedHosts[0]
	}
	return resp
}

// handleWatchWS is a read-only push feed for the TUI/widgets: sends the
// current snapshot immediately, then again on every registry mutation
// (agent connects/disconnects, BlueZ state changes) via broadcastState.
// No hello handshake needed since watchers never send commands.
func (c *Coordinator) handleWatchWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}

	c.mu.Lock()
	c.watchers[conn] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.watchers, conn)
		c.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	ctx := r.Context()
	if err := wsjson.Write(ctx, conn, c.currentState()); err != nil {
		return
	}

	// Watchers never send anything meaningful; block on reads purely to
	// detect the connection closing (client gone / network drop).
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}

// broadcastState pushes the current snapshot to every connected watcher.
// Best-effort: a watcher that fails to receive is dropped (its own read
// loop in handleWatchWS will notice the closed connection and clean up).
func (c *Coordinator) broadcastState() {
	state := c.currentState()

	c.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(c.watchers))
	for conn := range c.watchers {
		conns = append(conns, conn)
	}
	c.mu.Unlock()

	for _, conn := range conns {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = wsjson.Write(ctx, conn, state)
		cancel()
	}
}

type grabReq struct {
	Host string `json:"host"`
}

func (c *Coordinator) handleGrab(w http.ResponseWriter, r *http.Request) {
	var req grabReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Host == "" {
		writeErr(w, http.StatusBadRequest, "missing host")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cmdTimeout)
	defer cancel()

	if err := c.Grab(ctx, req.Host); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "holder": req.Host})
}

func (c *Coordinator) handleToggle(w http.ResponseWriter, r *http.Request) {
	var req grabReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Host == "" {
		writeErr(w, http.StatusBadRequest, "missing host")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := c.Toggle(ctx, req.Host)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Host    string `json:"host"`
		Playing *bool  `json:"playing,omitempty"`
	}{Host: req.Host, Playing: res.Playing})
}

// Grab evicts whichever agent currently reports Connected and connects the
// target host, re-verifying live state along the way rather than trusting
// the cache blindly.
func (c *Coordinator) Grab(ctx context.Context, targetHost string) error {
	c.mu.Lock()
	target, ok := c.agents[targetHost]
	var holders []*agentConn
	for host, a := range c.agents {
		if host != targetHost && a.connected {
			holders = append(holders, a)
		}
	}
	c.mu.Unlock()

	if !ok {
		return errHostOffline(targetHost)
	}

	for _, h := range holders {
		if _, err := c.sendCommand(ctx, h, proto.ActionDisconnect); err != nil {
			log.Printf("coordinator: evict %s failed: %v (continuing)", h.host, err)
		}
	}

	if _, err := c.sendCommand(ctx, target, proto.ActionConnect); err != nil {
		return err
	}
	return nil
}

// Toggle forwards a play/pause request to one live agent. The result carries
// its authoritative post-toggle playback state for the caller's UI feedback.
func (c *Coordinator) Toggle(ctx context.Context, host string) (proto.Envelope, error) {
	c.mu.Lock()
	agent, ok := c.agents[host]
	c.mu.Unlock()
	if !ok {
		return proto.Envelope{}, errHostOffline(host)
	}
	return c.sendCommand(ctx, agent, proto.ActionToggle)
}

// Media forwards one supported compact media control to a live agent.
func (c *Coordinator) Media(ctx context.Context, host, action string) (proto.Envelope, error) {
	if !isMediaAction(action) {
		return proto.Envelope{}, errUnsupportedAction(action)
	}
	c.mu.Lock()
	agent, ok := c.agents[host]
	c.mu.Unlock()
	if !ok {
		return proto.Envelope{}, errHostOffline(host)
	}
	return c.sendCommand(ctx, agent, action)
}

func isMediaAction(action string) bool {
	switch action {
	case proto.ActionVolumeDown, proto.ActionVolumeUp, proto.ActionPrevious, proto.ActionNext:
		return true
	default:
		return false
	}
}

func (c *Coordinator) sendCommand(ctx context.Context, a *agentConn, action string) (proto.Envelope, error) {
	reqID := time.Now().UTC().Format("20060102T150405.000000000")
	ch := make(chan proto.Envelope, 1)

	a.mu.Lock()
	a.pending[reqID] = ch
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.pending, reqID)
		a.mu.Unlock()
	}()

	if err := a.send(ctx, proto.Envelope{Type: proto.TypeCommand, ReqID: reqID, Action: action}); err != nil {
		return proto.Envelope{}, err
	}

	select {
	case res := <-ch:
		if !res.OK {
			return res, errCommandFailed(a.host, action, res.Error)
		}
		return res, nil
	case <-ctx.Done():
		return proto.Envelope{}, ctx.Err()
	}
}

func (c *Coordinator) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}

	runCtx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var env proto.Envelope
	readCtx, readCancel := context.WithTimeout(runCtx, 10*time.Second)
	err = wsjson.Read(readCtx, conn, &env)
	readCancel()
	if err != nil || env.Type != proto.TypeHello || env.Host == "" {
		conn.Close(websocket.StatusPolicyViolation, "expected hello")
		return
	}

	a := &agentConn{host: env.Host, conn: conn, controllerMAC: env.ControllerMAC, seenAt: time.Now(), pending: make(map[string]chan proto.Envelope)}
	c.mu.Lock()
	c.agents[a.host] = a
	c.mu.Unlock()
	log.Printf("coordinator: agent %q connected", a.host)
	c.broadcastState()

	defer func() {
		c.mu.Lock()
		if c.agents[a.host] == a {
			delete(c.agents, a.host)
		}
		c.mu.Unlock()
		log.Printf("coordinator: agent %q disconnected", a.host)
		c.broadcastState()
	}()

	go c.pingLoop(runCtx, a)

	for {
		var msg proto.Envelope
		if err := wsjson.Read(runCtx, conn, &msg); err != nil {
			return
		}
		c.mu.Lock()
		a.seenAt = time.Now()
		stateChanged := false
		if msg.Type == proto.TypeState {
			if msg.Connected != nil && a.connected != *msg.Connected {
				a.connected = *msg.Connected
				stateChanged = true
			}
			if msg.Playing != nil && a.playing != *msg.Playing {
				a.playing = *msg.Playing
				stateChanged = true
			}
			if msg.ActiveSource != nil {
				c.activeSource = *msg.ActiveSource
				if msg.SourceType != nil {
					c.sourceType = *msg.SourceType
				}
				c.sourceSeenAt = time.Now()
				stateChanged = true
			}
		}
		c.mu.Unlock()
		if stateChanged {
			c.broadcastState()
		}

		switch msg.Type {
		case proto.TypeState:
			// handled above under c.mu
		case proto.TypeResult:
			a.mu.Lock()
			ch, ok := a.pending[msg.ReqID]
			a.mu.Unlock()
			if ok {
				ch <- msg
			}
		}
	}
}

func (c *Coordinator) pingLoop(ctx context.Context, a *agentConn) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := a.conn.Ping(pctx)
			cancel()
			if err != nil {
				a.conn.Close(websocket.StatusGoingAway, "ping failed")
				return
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func errHostOffline(host string) error {
	return fmt.Errorf("host %q is not connected to the coordinator", host)
}

func errCommandFailed(host, action, msg string) error {
	return fmt.Errorf("%s on %q failed: %s", action, host, msg)
}

func errUnsupportedAction(action string) error {
	return fmt.Errorf("unsupported media action %q", action)
}
