package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// extractToken gets relay token from query param, header, or cookie.
func extractToken(r *http.Request) string {
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	if t := r.Header.Get("x-openclaw-relay-token"); t != "" {
		return t
	}
	if t := r.Header.Get("Authorization"); strings.HasPrefix(t, "Bearer ") {
		return strings.TrimPrefix(t, "Bearer ")
	}
	return ""
}

// deriveWSScheme returns "ws" or "wss" based on the request.
func deriveWSScheme(r *http.Request) string {
	if r.TLS != nil {
		return "wss"
	}
	if fwd := r.Header.Get("X-Forwarded-Proto"); fwd == "https" {
		return "wss"
	}
	return "ws"
}

// HandleCDPJsonVersion returns a CDP-compatible /json/version response.
func (rl *Relay) HandleCDPJsonVersion(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token == "" {
		http.Error(w, `{"error":"missing token"}`, 401)
		return
	}
	sess := rl.sessions.GetByToken(token)
	if sess == nil {
		http.Error(w, `{"error":"invalid token"}`, 401)
		return
	}

	wsScheme := deriveWSScheme(r)
	wsURL := fmt.Sprintf("%s://%s/relay/cdp/ws?token=%s", wsScheme, r.Host, token)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"Browser":              "Chrome/Relay",
		"Protocol-Version":     "1.3",
		"User-Agent":           "Vel-Relay/1.0",
		"webSocketDebuggerUrl": wsURL,
	})
}

// HandleCDPJsonList returns a CDP-compatible target list with per-target WS URLs.
func (rl *Relay) HandleCDPJsonList(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token == "" {
		http.Error(w, `{"error":"missing token"}`, 401)
		return
	}
	sess := rl.sessions.GetByToken(token)
	if sess == nil {
		http.Error(w, `{"error":"invalid token"}`, 401)
		return
	}

	targets := sess.GetTargets()
	if targets == nil {
		targets = []CDPTarget{}
	}

	wsScheme := deriveWSScheme(r)

	type cdpTarget struct {
		CDPTarget
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	out := make([]cdpTarget, len(targets))
	for i, t := range targets {
		out[i] = cdpTarget{
			CDPTarget:            t,
			WebSocketDebuggerURL: fmt.Sprintf("%s://%s/relay/cdp/page/%s?token=%s", wsScheme, r.Host, t.ID, token),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// cdpMessage is a minimal CDP JSON-RPC message for parsing.
type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

// HandleCDPProxyWS is a raw CDP WebSocket proxy.
// It speaks standard CDP JSON-RPC (no envelope) on the agent side,
// and translates to/from our relay envelope format.
//
// Flow:
// 1. Agent connects, gets list of targets
// 2. Agent sends Target.attachToTarget → proxy sends "connect" envelope to bridge
// 3. Bridge attaches and reports sessionId back via CDP event
// 4. Subsequent CDP messages with sessionId get wrapped and forwarded
// 5. Responses from bridge get unwrapped and sent as raw CDP to agent
func (rl *Relay) HandleCDPProxyWS(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token == "" {
		http.Error(w, "Unauthorized", 401)
		return
	}
	sess := rl.sessions.GetByToken(token)
	if sess == nil {
		http.Error(w, "Invalid token", 401)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[relay-cdp] WS upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("[relay-cdp] CDP proxy connected for user %d", sess.UserID)

	// Create a CDP proxy state
	proxy := &cdpProxy{
		conn:      conn,
		sess:      sess,
		relay:     rl,
		pendingID: make(map[int64]string), // maps CDP request id to method name
	}

	// Replace agent WS with a proxy-aware connection
	sess.SetAgentWS(conn)
	sess.mu.Lock()
	sess.CDPRawMode = true
	sess.mu.Unlock()

	defer func() {
		sess.ClearAgentWS()
		sess.mu.Lock()
		sess.CDPRawMode = false
		sess.mu.Unlock()
		log.Printf("[relay-cdp] CDP proxy disconnected for user %d", sess.UserID)
	}()

	// Read loop: agent sends raw CDP
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		proxy.handleAgentMessage(msg)
	}
}

type cdpProxy struct {
	conn      *websocket.Conn
	sess      *RelaySession
	relay     *Relay
	mu        sync.Mutex
	pendingID map[int64]string // CDP id -> method for tracking
}

func (p *cdpProxy) handleAgentMessage(raw []byte) {
	var msg cdpMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	p.mu.Lock()
	p.pendingID[msg.ID] = msg.Method
	p.mu.Unlock()

	p.sess.IncrementMsgCount()

	// Special handling for Target.getTargets - respond from cache
	if msg.Method == "Target.getTargets" {
		targets := p.sess.GetTargets()
		if targets == nil {
			targets = []CDPTarget{}
		}
		// Convert to CDP format
		type targetInfo struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			Title    string `json:"title"`
			URL      string `json:"url"`
			Attached bool   `json:"attached"`
		}
		infos := make([]targetInfo, len(targets))
		for i, t := range targets {
			infos[i] = targetInfo{
				TargetID: t.ID,
				Type:     t.Type,
				Title:    t.Title,
				URL:      t.URL,
			}
		}
		resp := map[string]interface{}{
			"id":     msg.ID,
			"result": map[string]interface{}{"targetInfos": infos},
		}
		respBytes, _ := json.Marshal(resp)
		p.conn.WriteMessage(websocket.TextMessage, respBytes)
		return
	}

	// Special handling for Target.attachToTarget - send "connect" envelope
	if msg.Method == "Target.attachToTarget" {
		var params struct {
			TargetID string `json:"targetId"`
			Flatten  bool   `json:"flatten"`
		}
		json.Unmarshal(msg.Params, &params)

		// Send connect envelope to bridge
		envelope := Envelope{
			Type:     "connect",
			TargetID: params.TargetID,
		}
		envBytes, _ := json.Marshal(envelope)
		p.sess.mu.Lock()
		browserWS := p.sess.BrowserWS
		p.sess.mu.Unlock()
		if browserWS != nil {
			browserWS.WriteMessage(websocket.TextMessage, envBytes)
		}

		// The bridge will send back a Target.attachedToTarget event via CDP
		// which will flow through the normal relay → agent path.
		// We store the request ID so we can synthesize a response when we see the event.
		return
	}

	// All other CDP messages: wrap in envelope and forward to browser
	// The message may have a sessionId (for target-specific commands)
	envelope := Envelope{
		Type: "cdp",
		Data: json.RawMessage(raw),
	}
	if msg.SessionID != "" {
		envelope.TargetID = "" // bridge uses sessionId from the CDP message itself
	}
	envBytes, _ := json.Marshal(envelope)

	p.sess.mu.Lock()
	browserWS := p.sess.BrowserWS
	p.sess.mu.Unlock()
	if browserWS != nil {
		browserWS.WriteMessage(websocket.TextMessage, envBytes)
	}
}

// HandleCDPStatusJSON returns enhanced status for CDP integration.
func (rl *Relay) HandleCDPStatusJSON(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token == "" {
		rl.HandleStatus(w, r)
		return
	}
	sess := rl.sessions.GetByToken(token)
	if sess == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"state":   "disconnected",
			"targets": []CDPTarget{},
		})
		return
	}

	sess.mu.Lock()
	browserConnected := sess.BrowserWS != nil
	agentConnected := sess.AgentWS != nil
	msgCount := sess.MsgCount
	targets := sess.Targets
	connAt := sess.ConnectedAt
	lastAct := sess.LastActivity
	sess.mu.Unlock()

	state := "disconnected"
	if browserConnected && agentConnected {
		state = "agent_active"
	} else if browserConnected {
		state = "connected"
	}

	if targets == nil {
		targets = []CDPTarget{}
	}

	resp := map[string]interface{}{
		"state":    state,
		"msgCount": msgCount,
		"targets":  targets,
	}
	if browserConnected {
		resp["connectedSince"] = connAt
		resp["lastActivity"] = lastAct
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
