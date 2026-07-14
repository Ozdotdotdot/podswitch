// Package coordinator runs on the always-on switch server. Agents dial in
// over a persistent WebSocket; the coordinator keeps a cache of who's
// online and who currently holds the AirPods, and orchestrates handoffs
// (evict current holder, connect target) on grab requests.
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
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/Ozdotdotdot/podswitch/internal/proto"
)

const (
	pingInterval = 20 * time.Second
	cmdTimeout   = 15 * time.Second
)

// agentConn is one connected agent.
type agentConn struct {
	host      string
	conn      *websocket.Conn
	connected bool // last known BlueZ Connected state on this host
	seenAt    time.Time

	mu      sync.Mutex
	pending map[string]chan proto.Envelope // reqID -> result channel
}

func (a *agentConn) send(ctx context.Context, env proto.Envelope) error {
	return wsjson.Write(ctx, a.conn, env)
}

// Coordinator holds the live agent registry and serves the HTTP+WS API.
type Coordinator struct {
	mu     sync.Mutex
	agents map[string]*agentConn // host -> conn
}

// New creates an empty Coordinator.
func New() *Coordinator {
	return &Coordinator{agents: make(map[string]*agentConn)}
}

// Handler returns the HTTP mux: WS accept endpoint + curl-able JSON API.
func (c *Coordinator) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/agent", c.handleAgentWS)
	mux.HandleFunc("GET /api/state", c.handleState)
	mux.HandleFunc("POST /api/grab", c.handleGrab)
	return mux
}

type stateResp struct {
	Holder string        `json:"holder,omitempty"` // best-known current holder, "" if none known online
	Agents []agentStatus `json:"agents"`
}

type agentStatus struct {
	Host      string `json:"host"`
	Online    bool   `json:"online"`
	Connected bool   `json:"connected"`
	SeenAt    string `json:"seenAt,omitempty"`
}

func (c *Coordinator) handleState(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()

	resp := stateResp{}
	for host, a := range c.agents {
		resp.Agents = append(resp.Agents, agentStatus{
			Host:      host,
			Online:    true,
			Connected: a.connected,
			SeenAt:    a.seenAt.Format(time.RFC3339),
		})
		if a.connected {
			resp.Holder = host
		}
	}
	writeJSON(w, http.StatusOK, resp)
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

	a := &agentConn{host: env.Host, conn: conn, seenAt: time.Now(), pending: make(map[string]chan proto.Envelope)}
	c.mu.Lock()
	c.agents[a.host] = a
	c.mu.Unlock()
	log.Printf("coordinator: agent %q connected", a.host)

	defer func() {
		c.mu.Lock()
		if c.agents[a.host] == a {
			delete(c.agents, a.host)
		}
		c.mu.Unlock()
		log.Printf("coordinator: agent %q disconnected", a.host)
	}()

	go c.pingLoop(runCtx, a)

	for {
		var msg proto.Envelope
		if err := wsjson.Read(runCtx, conn, &msg); err != nil {
			return
		}
		c.mu.Lock()
		a.seenAt = time.Now()
		if msg.Type == proto.TypeState && msg.Connected != nil {
			a.connected = *msg.Connected
		}
		c.mu.Unlock()

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
