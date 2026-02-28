package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// CDPTarget represents a Chrome DevTools Protocol target (tab).
type CDPTarget struct {
	Description          string `json:"description"`
	DevtoolsFrontendURL  string `json:"devtoolsFrontendUrl,omitempty"`
	ID                   string `json:"id"`
	Title                string `json:"title"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl,omitempty"`
}

// RelaySession holds state for one user's relay connection.
type RelaySession struct {
	UserID       int64
	Token        string
	BrowserWS    *websocket.Conn
	AgentWS      *websocket.Conn
	Targets      []CDPTarget
	ConnectedAt  time.Time
	LastActivity time.Time
	MsgCount     int64
	CDPRawMode   bool // when true, agent WS speaks raw CDP (no envelope)
	mu           sync.Mutex
}

// SessionManager manages relay sessions keyed by token.
type SessionManager struct {
	mu       sync.RWMutex
	byToken  map[string]*RelaySession
	byUserID map[int64]*RelaySession
}

// NewSessionManager creates a new manager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		byToken:  make(map[string]*RelaySession),
		byUserID: make(map[int64]*RelaySession),
	}
}

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// GetOrCreateToken returns existing token for user or creates new session.
func (sm *SessionManager) GetOrCreateToken(userID int64) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if s, ok := sm.byUserID[userID]; ok {
		return s.Token
	}

	token := generateToken()
	s := &RelaySession{
		UserID:      userID,
		Token:       token,
		ConnectedAt: time.Now(),
	}
	sm.byToken[token] = s
	sm.byUserID[userID] = s
	return token
}

// GetByToken returns session by token.
func (sm *SessionManager) GetByToken(token string) *RelaySession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.byToken[token]
}

// SetBrowserWS sets the browser websocket for a session.
func (s *RelaySession) SetBrowserWS(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.BrowserWS != nil {
		s.BrowserWS.Close()
	}
	s.BrowserWS = conn
	s.ConnectedAt = time.Now()
}

// SetAgentWS sets the agent websocket for a session.
func (s *RelaySession) SetAgentWS(conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.AgentWS != nil {
		s.AgentWS.Close()
	}
	s.AgentWS = conn
}

// SetTargets updates the cached target list.
func (s *RelaySession) SetTargets(targets []CDPTarget) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Targets = targets
	s.LastActivity = time.Now()
}

// GetTargets returns cached targets.
func (s *RelaySession) GetTargets() []CDPTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Targets
}

// IncrementMsgCount increments and returns the message count.
func (s *RelaySession) IncrementMsgCount() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MsgCount++
	s.LastActivity = time.Now()
	return s.MsgCount
}

// ClearBrowserWS clears the browser websocket.
func (s *RelaySession) ClearBrowserWS() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BrowserWS = nil
}

// ClearAgentWS clears the agent websocket.
func (s *RelaySession) ClearAgentWS() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AgentWS = nil
}
