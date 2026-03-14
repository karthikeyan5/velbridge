package server

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	bolt "go.etcd.io/bbolt"

	vel "vel/pkg/vel"
)

var relaySessionsBucket = []byte("relay_sessions")

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

// persistedSession is the subset of RelaySession that survives restarts.
type persistedSession struct {
	Token       string    `json:"token"`
	UserID      int64     `json:"userId"`
	ConnectedAt time.Time `json:"connectedAt"`
}

// SessionManager manages relay sessions keyed by token.
type SessionManager struct {
	mu       sync.RWMutex
	byToken  map[string]*RelaySession
	byUserID map[int64]*RelaySession
	db       *bolt.DB
}

// NewSessionManager creates a new manager (in-memory only, for tests).
func NewSessionManager() *SessionManager {
	return &SessionManager{
		byToken:  make(map[string]*RelaySession),
		byUserID: make(map[int64]*RelaySession),
	}
}

// NewSessionManagerWithDB creates a manager backed by BoltDB for persistence.
func NewSessionManagerWithDB(db *bolt.DB) *SessionManager {
	sm := &SessionManager{
		byToken:  make(map[string]*RelaySession),
		byUserID: make(map[int64]*RelaySession),
		db:       db,
	}

	// Ensure bucket exists
	if db != nil {
		db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists(relaySessionsBucket)
			return err
		})
		sm.loadFromDB()
	}

	return sm
}

// loadFromDB restores persisted sessions into memory on startup.
func (sm *SessionManager) loadFromDB() {
	if sm.db == nil {
		return
	}
	sm.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(relaySessionsBucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var ps persistedSession
			if err := json.Unmarshal(v, &ps); err != nil {
				log.Printf("[relay] skipping corrupted session %s: %v", string(k), err)
				return nil
			}
			s := &RelaySession{
				UserID:      ps.UserID,
				Token:       ps.Token,
				ConnectedAt: ps.ConnectedAt,
			}
			sm.byToken[ps.Token] = s
			sm.byUserID[ps.UserID] = s
			log.Printf("[relay] restored session for user %d (token %s...)", ps.UserID, ps.Token[:8])
			return nil
		})
	})
}

// persistSession writes a session to BoltDB.
func (sm *SessionManager) persistSession(s *RelaySession) {
	if sm.db == nil {
		return
	}
	ps := persistedSession{
		Token:       s.Token,
		UserID:      s.UserID,
		ConnectedAt: s.ConnectedAt,
	}
	data, err := json.Marshal(ps)
	if err != nil {
		log.Printf("[relay] failed to marshal session for user %d: %v", s.UserID, err)
		return
	}
	sm.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(relaySessionsBucket)
		if b == nil {
			return nil
		}
		return b.Put([]byte(s.Token), data)
	})
}

func generateToken() string {
	token, err := vel.GenerateToken(16)
	if err != nil {
		// Fallback should never happen — crypto/rand failure is fatal-grade.
		panic("vel.GenerateToken failed: " + err.Error())
	}
	return token
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

	// Persist to BoltDB
	sm.persistSession(s)

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
