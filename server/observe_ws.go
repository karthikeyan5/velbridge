package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	vel "vel/pkg/vel"

	"github.com/gorilla/websocket"
)

// observeWSClient represents a WebSocket connection in observe mode.
type observeWSClient struct {
	conn      *websocket.Conn
	sessionID string
	role      string // "user" or "agent"
}

// observeWSManager tracks active observe-mode WebSocket connections.
type observeWSManager struct {
	mu      sync.RWMutex
	clients map[string][]*observeWSClient // sessionID → clients
}

func newObserveWSManager() *observeWSManager {
	return &observeWSManager{clients: make(map[string][]*observeWSClient)}
}

func (m *observeWSManager) Add(sessionID string, client *observeWSClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[sessionID] = append(m.clients[sessionID], client)
}

func (m *observeWSManager) Remove(sessionID string, conn *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	clients := m.clients[sessionID]
	for i, c := range clients {
		if c.conn == conn {
			m.clients[sessionID] = append(clients[:i], clients[i+1:]...)
			break
		}
	}
	if len(m.clients[sessionID]) == 0 {
		delete(m.clients, sessionID)
	}
}

func (m *observeWSManager) Broadcast(sessionID string, msg interface{}) {
	m.mu.RLock()
	clients := m.clients[sessionID]
	m.mu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	for _, c := range clients {
		c.conn.WriteMessage(websocket.TextMessage, data)
	}
}

func (m *observeWSManager) BroadcastToRole(sessionID, role string, msg interface{}) {
	m.mu.RLock()
	clients := m.clients[sessionID]
	m.mu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	for _, c := range clients {
		if c.role == role {
			c.conn.WriteMessage(websocket.TextMessage, data)
		}
	}
}

func (m *observeWSManager) HasRole(sessionID, role string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.clients[sessionID] {
		if c.role == role {
			return true
		}
	}
	return false
}

// HandleObserveUserWS handles the user's (browser) WebSocket for observe mode.
func (rl *Relay) HandleObserveUserWS(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "Missing session parameter", 400)
		return
	}

	sess := rl.observeSessions.Get(sessionID)
	if sess == nil {
		http.Error(w, "Session not found", 404)
		return
	}

	// Verify authentication: require a valid Vel user/API-key auth context,
	// or an explicit bot token for agent-driven observe sessions.
	authHeader := r.Header.Get("Authorization")
	hasAuth := vel.Check(r) != nil
	if !hasAuth && strings.HasPrefix(authHeader, "Bearer ") {
		botToken := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		hasAuth = vel.CheckBotToken(botToken)
	}
	if !hasAuth {
		http.Error(w, "Unauthorized", 401)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[observe-ws] user upgrade failed: %v", err)
		return
	}

	client := &observeWSClient{conn: conn, sessionID: sessionID, role: "user"}
	rl.observeWSClients.Add(sessionID, client)

	sess.mu.Lock()
	sess.UserConnected = true
	sess.LastActivity = time.Now()
	sess.mu.Unlock()

	log.Printf("[observe-ws] user connected, session=%s", sessionID)

	// Notify agent
	rl.observeWSClients.BroadcastToRole(sessionID, "agent", map[string]interface{}{
		"type":   "user_connected",
		"status": true,
	})

	// Send initial state to user
	sess.mu.Lock()
	initMsg := map[string]interface{}{
		"type":            "init",
		"session_id":      sessionID,
		"mode":            sess.Mode,
		"agent_connected": sess.AgentConnected,
		"latest_message":  sess.LatestMessage,
	}
	sess.mu.Unlock()
	conn.WriteJSON(initMsg)

	defer func() {
		conn.Close()
		rl.observeWSClients.Remove(sessionID, conn)

		sess.mu.Lock()
		sess.UserConnected = rl.observeWSClients.HasRole(sessionID, "user")
		sess.mu.Unlock()

		// Notify agent
		rl.observeWSClients.BroadcastToRole(sessionID, "agent", map[string]interface{}{
			"type":   "user_connected",
			"status": false,
		})

		log.Printf("[observe-ws] user disconnected, session=%s", sessionID)
	}()

	// Read loop
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var env map[string]interface{}
		if err := json.Unmarshal(msg, &env); err != nil {
			continue
		}

		msgType, _ := env["type"].(string)
		switch msgType {
		case "screenshot":
			// User sent a screenshot
			dataURL, _ := env["data"].(string)
			method, _ := env["method"].(string)
			if dataURL != "" {
				path, err := rl.observeSessions.SaveScreenshot(sessionID, dataURL)
				if err != nil {
					log.Printf("[observe-ws] screenshot save failed: %v", err)
					continue
				}

				// Notify agent via WebSocket
				rl.observeWSClients.BroadcastToRole(sessionID, "agent", map[string]interface{}{
					"type":   "screenshot",
					"path":   path,
					"method": method,
					"ts":     time.Now().UnixMilli(),
				})

				// Trigger OpenClaw webhook to wake agent
				rl.triggerAgentWebhook(sess, "screenshot", path)
			}

		case "frame":
			// Live stream frame from user
			frameData, _ := env["data"].(string)
			if frameData != "" {
				sess.mu.Lock()
				sess.DataTransferred += int64(len(frameData))
				sess.LastActivity = time.Now()
				sess.mu.Unlock()

				// Forward frame to agent
				rl.observeWSClients.BroadcastToRole(sessionID, "agent", map[string]interface{}{
					"type": "frame",
					"data": frameData,
					"ts":   time.Now().UnixMilli(),
				})
			}

		case "user_message":
			// User sent a text response (quick button or custom)
			text, _ := env["text"].(string)
			if text != "" {
				sess.mu.Lock()
				sess.LastActivity = time.Now()
				sess.mu.Unlock()

				// Forward to agent
				rl.observeWSClients.BroadcastToRole(sessionID, "agent", map[string]interface{}{
					"type": "user_message",
					"text": text,
					"ts":   time.Now().UnixMilli(),
				})

				// Trigger webhook
				rl.triggerAgentWebhook(sess, "message", text)
			}

		case "stream_started":
			log.Printf("[observe-ws] stream started for session %s", sessionID)
			rl.observeWSClients.BroadcastToRole(sessionID, "agent", map[string]interface{}{
				"type": "stream_started",
			})

		case "stream_stopped":
			log.Printf("[observe-ws] stream stopped for session %s", sessionID)
			rl.observeWSClients.BroadcastToRole(sessionID, "agent", map[string]interface{}{
				"type": "stream_stopped",
			})

		case "pong":
			// keepalive

		case "audio":
			// Audio data from user
			rl.observeWSClients.BroadcastToRole(sessionID, "agent", env)
		}
	}
}

// HandleObserveAgentWS handles the agent-side WebSocket for observe mode.
// This is the /api/bridge/observe/{id}/stream endpoint.
func (rl *Relay) HandleObserveAgentWS(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from path: /api/bridge/observe/{id}/stream
	path := strings.TrimPrefix(r.URL.Path, "/api/bridge/observe/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] != "stream" {
		http.Error(w, "Invalid path", 400)
		return
	}
	sessionID := parts[0]

	sess := rl.observeSessions.Get(sessionID)
	if sess == nil {
		http.Error(w, "Session not found", 404)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[observe-ws] agent upgrade failed: %v", err)
		return
	}

	client := &observeWSClient{conn: conn, sessionID: sessionID, role: "agent"}
	rl.observeWSClients.Add(sessionID, client)

	sess.mu.Lock()
	sess.AgentConnected = true
	sess.LastActivity = time.Now()
	sess.mu.Unlock()

	log.Printf("[observe-ws] agent connected, session=%s", sessionID)

	// Notify user
	rl.observeWSClients.BroadcastToRole(sessionID, "user", map[string]interface{}{
		"type":   "agent_connected",
		"status": true,
	})

	defer func() {
		conn.Close()
		rl.observeWSClients.Remove(sessionID, conn)

		sess.mu.Lock()
		sess.AgentConnected = rl.observeWSClients.HasRole(sessionID, "agent")
		sess.mu.Unlock()

		rl.observeWSClients.BroadcastToRole(sessionID, "user", map[string]interface{}{
			"type":   "agent_connected",
			"status": false,
		})

		log.Printf("[observe-ws] agent disconnected, session=%s", sessionID)
	}()

	// Read loop: agent sends commands
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var env map[string]interface{}
		if err := json.Unmarshal(msg, &env); err != nil {
			continue
		}

		msgType, _ := env["type"].(string)
		switch msgType {
		case "request_screenshot":
			// Agent requests a screenshot from user
			rl.observeWSClients.BroadcastToRole(sessionID, "user", map[string]interface{}{
				"type": "take_screenshot",
			})

		case "agent_message":
			// Agent sends text to user
			text, _ := env["text"].(string)
			sess.mu.Lock()
			sess.LatestMessage = text
			sess.LastActivity = time.Now()
			sess.mu.Unlock()

			buttons, _ := env["buttons"].([]interface{})
			outMsg := map[string]interface{}{
				"type": "agent_message",
				"text": text,
				"ts":   time.Now().UnixMilli(),
			}
			if len(buttons) > 0 {
				outMsg["buttons"] = buttons
			}
			rl.observeWSClients.BroadcastToRole(sessionID, "user", outMsg)

		case "end_session":
			rl.observeWSClients.Broadcast(sessionID, map[string]interface{}{
				"type": "session_ended",
			})
			rl.observeSessions.Remove(sessionID)
			return

		case "pong":
			// keepalive
		}
	}
}

// triggerAgentWebhook calls the OpenClaw webhook to wake the agent.
func (rl *Relay) triggerAgentWebhook(sess *ObserveSession, eventType, data string) {
	if rl.openclawGateway == "" || rl.openclawToken == "" {
		return
	}

	sessionKey := sess.TargetSessionKey
	if sessionKey == "" {
		return
	}

	var message string
	switch eventType {
	case "screenshot":
		message = fmt.Sprintf("[Observe session %s] Screenshot received. Path: %s", sess.ID, data)
	case "message":
		message = fmt.Sprintf("[Observe session %s] User response: %s", sess.ID, data)
	default:
		message = fmt.Sprintf("[Observe session %s] Event: %s", sess.ID, eventType)
	}

	payload := map[string]interface{}{
		"message":    message,
		"sessionKey": sessionKey,
		"wakeMode":   "now",
	}

	if eventType == "screenshot" {
		payload["imagePath"] = data
	}

	body, _ := json.Marshal(payload)
	url := rl.openclawGateway + "/hooks/agent"

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[observe] webhook request failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+rl.openclawToken)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[observe] webhook call failed: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("[observe] webhook triggered for session %s, event=%s, status=%d", sess.ID, eventType, resp.StatusCode)
}

// Ensure imports are used
var _ = os.Stderr
var _ vel.AppConfig
