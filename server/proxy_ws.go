package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"

	"github.com/gorilla/websocket"
)

// proxyWSClient holds a WebSocket connection from the browser in proxy mode.
type proxyWSClient struct {
	conn      *websocket.Conn
	sessionID string
	domain    string
}

// proxyWSManager tracks active proxy-mode WS connections.
type proxyWSManager struct {
	mu      sync.RWMutex
	clients map[string]*proxyWSClient // sessionID → client
}

func newProxyWSManager() *proxyWSManager {
	return &proxyWSManager{clients: make(map[string]*proxyWSClient)}
}

func (m *proxyWSManager) Add(sessionID string, client *proxyWSClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Close existing if any
	if old, ok := m.clients[sessionID]; ok {
		old.conn.Close()
	}
	m.clients[sessionID] = client
}

func (m *proxyWSManager) Remove(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clients, sessionID)
}

func (m *proxyWSManager) Get(sessionID string) *proxyWSClient {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clients[sessionID]
}

// SendCommand sends a command to the browser via the proxy WS connection.
func (m *proxyWSManager) SendCommand(sessionID string, cmd interface{}) error {
	m.mu.RLock()
	client := m.clients[sessionID]
	m.mu.RUnlock()
	if client == nil {
		return nil
	}
	return client.conn.WriteJSON(cmd)
}

// HandleProxyWS handles the browser-side WebSocket for proxy mode.
// Browser's injected JS connects here to send console/network/screenshot data.
func (rl *Relay) HandleProxyWS(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "Missing session parameter", 400)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[proxy-ws] upgrade failed: %v", err)
		return
	}

	client := &proxyWSClient{
		conn:      conn,
		sessionID: sessionID,
	}
	rl.proxyWSClients.Add(sessionID, client)
	log.Printf("[proxy-ws] browser connected, session=%s", sessionID)

	defer func() {
		conn.Close()
		rl.proxyWSClients.Remove(sessionID)
		log.Printf("[proxy-ws] browser disconnected, session=%s", sessionID)
	}()

	// Read loop: receive data from the browser (console, errors, network, screenshots)
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		// Parse and handle messages
		var env map[string]interface{}
		if err := json.Unmarshal(msg, &env); err != nil {
			continue
		}

		msgType, _ := env["type"].(string)
		switch msgType {
		case "console", "error", "net":
			// Store in session log (future: forward to agent via WS)
			log.Printf("[proxy-ws] [%s] %s: %s", sessionID, msgType, string(msg))

		case "screenshot":
			// Screenshot received from browser
			log.Printf("[proxy-ws] [%s] screenshot received (%d bytes data)", sessionID, len(msg))

		case "info":
			// Page info response
			log.Printf("[proxy-ws] [%s] info: %s", sessionID, string(msg))

		case "recording":
			// Recording data
			log.Printf("[proxy-ws] [%s] recording received", sessionID)

		case "import_cookies":
			// Cookie import from OAuth overlay
			cookies, _ := env["cookies"].(string)
			domain, _ := env["domain"].(string)
			if domain == "" {
				domain = client.domain
			}
			if cookies != "" && domain != "" {
				sess := rl.proxySessions.Get(domain)
				if sess != nil {
					log.Printf("[proxy-ws] importing cookies for domain %s", domain)
				}
			}

		case "pong":
			// keepalive response

		default:
			// Forward unknown messages to any connected agent for this session
			log.Printf("[proxy-ws] [%s] unknown type: %s", sessionID, msgType)
		}
	}
}

// HandleProxyWSRelay is the WebSocket-to-WebSocket relay endpoint.
// When the target site uses WebSockets, the injected JS rewrites the WS URL
// to go through this endpoint, which relays to the actual target.
func (rl *Relay) HandleProxyWSRelay(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("target")
	if targetURL == "" {
		http.Error(w, "Missing target parameter", 400)
		return
	}

	// Parse and validate target URL
	parsed, err := url.Parse(targetURL)
	if err != nil {
		http.Error(w, "Invalid target URL", 400)
		return
	}

	// Security: block local targets
	host := parsed.Hostname()
	if isBlockedTarget(host) {
		http.Error(w, "WebSocket relay to local/private networks is blocked", 403)
		return
	}

	// Connect to the target WebSocket
	dialer := websocket.DefaultDialer
	targetConn, _, err := dialer.Dial(targetURL, nil)
	if err != nil {
		log.Printf("[proxy-wsproxy] failed to connect to %s: %v", targetURL, err)
		http.Error(w, "Failed to connect to target WebSocket", 502)
		return
	}
	defer targetConn.Close()

	// Upgrade the client connection
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[proxy-wsproxy] client upgrade failed: %v", err)
		return
	}
	defer clientConn.Close()

	log.Printf("[proxy-wsproxy] relay started: client ↔ %s", targetURL)

	// Bidirectional relay
	done := make(chan struct{}, 2)

	// Client → Target
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			msgType, msg, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			if err := targetConn.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	}()

	// Target → Client
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			msgType, msg, err := targetConn.ReadMessage()
			if err != nil {
				return
			}
			if err := clientConn.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	}()

	<-done
	log.Printf("[proxy-wsproxy] relay ended: %s", targetURL)
}

// HandleProxyAgentWS handles agent-side WebSocket for controlling proxy sessions.
// Agents connect here to send commands (screenshot, navigate, etc.) to the proxied page.
func (rl *Relay) HandleProxyAgentWS(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "Missing session parameter", 400)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[proxy-agent-ws] upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("[proxy-agent-ws] agent connected, session=%s", sessionID)

	// Read loop: receive commands from agent, forward to browser
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var cmd map[string]interface{}
		if err := json.Unmarshal(msg, &cmd); err != nil {
			continue
		}

		// Forward command to the browser client
		client := rl.proxyWSClients.Get(sessionID)
		if client != nil {
			if err := client.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Printf("[proxy-agent-ws] failed to forward to browser: %v", err)
			}
		}
	}
	log.Printf("[proxy-agent-ws] agent disconnected, session=%s", sessionID)
}

// Ensure io is used (for the relay functions above)
var _ = io.EOF
