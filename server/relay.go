package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	vel "vel/pkg/vel"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Relay holds the relay state and handlers.
type Relay struct {
	sessions  *SessionManager
	pairing   *PairingManager
	launchers *launcherStore
}

// New creates a new Relay instance.
func New() *Relay {
	sm := NewSessionManager()
	return &Relay{
		sessions:  sm,
		pairing:   NewPairingManager(sm),
		launchers: newLauncherStore(),
	}
}

// Envelope is the message format between browser/agent and relay.
type Envelope struct {
	Type     string          `json:"type"`
	TargetID string          `json:"targetId,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Connected *bool          `json:"connected,omitempty"`
}

// HandleToken returns a relay token for the authenticated user.
func (rl *Relay) HandleToken(w http.ResponseWriter, r *http.Request) {
	user := vel.Check(r)
	if user == nil {
		http.Error(w, "Unauthorized", 401)
		return
	}
	token := rl.sessions.GetOrCreateToken(user.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

// HandleTargets returns cached target list for a token.
func (rl *Relay) HandleTargets(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		// Also try cookie auth
		user := vel.Check(r)
		if user == nil {
			http.Error(w, "Unauthorized", 401)
			return
		}
		token = rl.sessions.GetOrCreateToken(user.ID)
	}
	sess := rl.sessions.GetByToken(token)
	if sess == nil {
		http.Error(w, "Invalid token", 401)
		return
	}
	targets := sess.GetTargets()
	if targets == nil {
		targets = []CDPTarget{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(targets)
}

// HandleBrowserWS handles the browser-side WebSocket connection.
func (rl *Relay) HandleBrowserWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	sess := rl.sessions.GetByToken(token)
	if sess == nil {
		http.Error(w, "Invalid token", 401)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[relay] browser WS upgrade failed: %v", err)
		return
	}
	sess.SetBrowserWS(conn)
	log.Printf("[relay] browser connected for user %d", sess.UserID)

	// Notify agent if connected
	sess.mu.Lock()
	agentWS := sess.AgentWS
	sess.mu.Unlock()
	if agentWS != nil {
		connected := true
		agentWS.WriteJSON(Envelope{Type: "status", Connected: &connected})
	}

	// Start keepalive
	go rl.keepalive(sess, conn, true)

	// Read loop
	defer func() {
		conn.Close()
		sess.ClearBrowserWS()
		log.Printf("[relay] browser disconnected for user %d", sess.UserID)
		// Notify agent
		sess.mu.Lock()
		agentWS = sess.AgentWS
		sess.mu.Unlock()
		if agentWS != nil {
			connected := false
			agentWS.WriteJSON(Envelope{Type: "status", Connected: &connected})
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var env Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			continue
		}

		switch env.Type {
		case "targets":
			var targets []CDPTarget
			if err := json.Unmarshal(env.Data, &targets); err == nil {
				sess.SetTargets(targets)
			}
			// Forward to agent (skip in raw CDP mode — agent uses Target.getTargets)
			sess.mu.Lock()
			agentWS = sess.AgentWS
			rawMode := sess.CDPRawMode
			sess.mu.Unlock()
			if agentWS != nil && !rawMode {
				agentWS.WriteMessage(websocket.TextMessage, msg)
			}

		case "cdp":
			sess.IncrementMsgCount()
			// Forward to agent
			sess.mu.Lock()
			agentWS = sess.AgentWS
			rawMode := sess.CDPRawMode
			sess.mu.Unlock()
			if agentWS != nil {
				if rawMode {
					// Raw CDP mode: unwrap envelope, send just the CDP data
					agentWS.WriteMessage(websocket.TextMessage, env.Data)
				} else {
					agentWS.WriteMessage(websocket.TextMessage, msg)
				}
			}

		case "pong":
			// keepalive response, no-op
		}
	}
}

// HandleAgentWS handles the agent-side (OpenClaw) WebSocket connection.
func (rl *Relay) HandleAgentWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	sess := rl.sessions.GetByToken(token)
	if sess == nil {
		http.Error(w, "Invalid token", 401)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[relay] agent WS upgrade failed: %v", err)
		return
	}
	sess.SetAgentWS(conn)
	log.Printf("[relay] agent connected for user %d", sess.UserID)

	// Send initial status
	sess.mu.Lock()
	browserConnected := sess.BrowserWS != nil
	sess.mu.Unlock()
	conn.WriteJSON(Envelope{Type: "status", Connected: &browserConnected})

	// Send cached targets
	targets := sess.GetTargets()
	if len(targets) > 0 {
		targetsJSON, _ := json.Marshal(targets)
		conn.WriteJSON(Envelope{Type: "targets", Data: targetsJSON})
	}

	// Start keepalive
	go rl.keepalive(sess, conn, false)

	// Read loop
	defer func() {
		conn.Close()
		sess.ClearAgentWS()
		log.Printf("[relay] agent disconnected for user %d", sess.UserID)
		// Notify browser that agent disconnected
		sess.mu.Lock()
		browserWS := sess.BrowserWS
		sess.mu.Unlock()
		if browserWS != nil {
			browserWS.WriteJSON(Envelope{Type: "agent_disconnected"})
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var env Envelope
		if err := json.Unmarshal(msg, &env); err != nil {
			continue
		}

		switch env.Type {
		case "list":
			targets := sess.GetTargets()
			if targets == nil {
				targets = []CDPTarget{}
			}
			targetsJSON, _ := json.Marshal(targets)
			conn.WriteJSON(Envelope{Type: "targets", Data: targetsJSON})

		case "cdp", "connect", "disconnect":
			sess.IncrementMsgCount()
			// Forward to browser
			sess.mu.Lock()
			browserWS := sess.BrowserWS
			sess.mu.Unlock()
			if browserWS != nil {
				browserWS.WriteMessage(websocket.TextMessage, msg)
			}

		case "pong":
			// keepalive response
		}
	}
}

// HandleStatus returns relay status for the dashboard panel.
func (rl *Relay) HandleStatus(w http.ResponseWriter, r *http.Request) {
	user := vel.Check(r)
	if user == nil {
		http.Error(w, "Unauthorized", 401)
		return
	}
	token := rl.sessions.GetOrCreateToken(user.ID)
	sess := rl.sessions.GetByToken(token)
	if sess == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"state": "disconnected"})
		return
	}
	sess.mu.Lock()
	browserConnected := sess.BrowserWS != nil
	agentConnected := sess.AgentWS != nil
	connAt := sess.ConnectedAt
	msgCount := sess.MsgCount
	var activeTab string
	if len(sess.Targets) > 0 {
		activeTab = sess.Targets[0].Title
	}
	sess.mu.Unlock()

	state := "disconnected"
	if browserConnected && agentConnected {
		state = "agent_active"
	} else if browserConnected {
		state = "connected"
	}

	resp := map[string]interface{}{
		"state":    state,
		"msgCount": msgCount,
	}
	if browserConnected {
		resp["connectedSince"] = connAt.Format(time.RFC3339)
	}
	if agentConnected && activeTab != "" {
		resp["activeTab"] = activeTab
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// keepalive sends ping every 30s.
func (rl *Relay) keepalive(sess *RelaySession, conn *websocket.Conn, isBrowser bool) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		sess.mu.Lock()
		var current *websocket.Conn
		if isBrowser {
			current = sess.BrowserWS
		} else {
			current = sess.AgentWS
		}
		sess.mu.Unlock()

		if current != conn {
			return // connection was replaced
		}
		if err := conn.WriteJSON(Envelope{Type: "ping"}); err != nil {
			return
		}
	}
}
