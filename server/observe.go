package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ObserveSession represents an active observe-mode session.
type ObserveSession struct {
	ID               string    `json:"id"`
	Mode             string    `json:"mode"` // text, screenshot, stream
	Label            string    `json:"label"`
	CreatedAt        time.Time `json:"created_at"`
	Timeout          int       `json:"timeout"` // seconds, 0 = persistent
	TargetSessionKey string    `json:"target_session_key,omitempty"`

	// Runtime state
	UserConnected  bool   `json:"user_connected"`
	AgentConnected bool   `json:"agent_connected"`
	DataTransferred int64 `json:"data_transferred"`
	ScreenshotCount int   `json:"screenshot_count"`
	LastActivity   time.Time `json:"last_activity"`
	LatestMessage  string `json:"latest_message,omitempty"`

	// Settings (persisted agent-side)
	Quality    string `json:"quality"`     // auto, low, high, screenshots_only
	TabAudio   bool   `json:"tab_audio"`
	SysAudio   bool   `json:"sys_audio"`
	Persistent bool   `json:"persistent"`

	mu sync.Mutex
}

// observeManager manages active observe sessions.
type observeManager struct {
	mu       sync.RWMutex
	sessions map[string]*ObserveSession
	history  []*ObserveSession // last 10 completed
	dataDir  string            // for screenshots
	settings *observeSettings  // persisted defaults
}

// observeSettings holds saved default settings for observe mode.
type observeSettings struct {
	Quality    string `json:"quality"`
	TabAudio   bool   `json:"tab_audio"`
	SysAudio   bool   `json:"sys_audio"`
	Persistent bool   `json:"persistent"`
	Timeout    int    `json:"timeout"`
}

func newObserveManager(dataDir string) *observeManager {
	om := &observeManager{
		sessions: make(map[string]*ObserveSession),
		dataDir:  dataDir,
	}
	// Load saved settings
	om.settings = om.loadSettings()
	// Ensure screenshot directory exists
	os.MkdirAll(filepath.Join(dataDir, "observe"), 0755)
	go om.cleanup()
	return om
}

func (om *observeManager) settingsPath() string {
	return filepath.Join(om.dataDir, "observe", "settings.json")
}

func (om *observeManager) loadSettings() *observeSettings {
	s := &observeSettings{
		Quality: "auto",
		Timeout: 30,
	}
	data, err := os.ReadFile(om.settingsPath())
	if err != nil {
		return s
	}
	json.Unmarshal(data, s)
	return s
}

func (om *observeManager) saveSettings(s *observeSettings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(om.settingsPath(), data, 0644)
}

func (om *observeManager) Create(mode, label, sessionKey string, timeout int) *ObserveSession {
	om.mu.Lock()
	defer om.mu.Unlock()

	id := generateToken()[:12]

	if timeout <= 0 {
		timeout = om.settings.Timeout
	}

	sess := &ObserveSession{
		ID:               id,
		Mode:             mode,
		Label:            label,
		CreatedAt:        time.Now(),
		Timeout:          timeout,
		TargetSessionKey: sessionKey,
		Quality:          om.settings.Quality,
		TabAudio:         om.settings.TabAudio,
		SysAudio:         om.settings.SysAudio,
		Persistent:       om.settings.Persistent,
		LastActivity:     time.Now(),
	}
	om.sessions[id] = sess

	// Create screenshot dir for this session
	os.MkdirAll(filepath.Join(om.dataDir, "observe", id), 0755)

	return sess
}

func (om *observeManager) Get(id string) *ObserveSession {
	om.mu.RLock()
	defer om.mu.RUnlock()
	return om.sessions[id]
}

func (om *observeManager) List() []*ObserveSession {
	om.mu.RLock()
	defer om.mu.RUnlock()
	out := make([]*ObserveSession, 0, len(om.sessions))
	for _, s := range om.sessions {
		out = append(out, s)
	}
	return out
}

func (om *observeManager) History() []*ObserveSession {
	om.mu.RLock()
	defer om.mu.RUnlock()
	out := make([]*ObserveSession, len(om.history))
	copy(out, om.history)
	return out
}

func (om *observeManager) Remove(id string) {
	om.mu.Lock()
	defer om.mu.Unlock()
	if s, ok := om.sessions[id]; ok {
		// Add to history
		om.history = append(om.history, s)
		if len(om.history) > 10 {
			om.history = om.history[len(om.history)-10:]
		}
		delete(om.sessions, id)

		// Clean up screenshot files from disk
		sessionDir := filepath.Join(om.dataDir, "observe", id)
		if err := os.RemoveAll(sessionDir); err != nil {
			log.Printf("[observe] failed to clean up screenshots for session %s: %v", id, err)
		}
	}
}

func (om *observeManager) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		om.mu.Lock()
		now := time.Now()
		for id, s := range om.sessions {
			s.mu.Lock()
			isStale := !s.Persistent && s.Timeout > 0 &&
				now.Sub(s.LastActivity) > time.Duration(s.Timeout)*time.Second &&
				!s.UserConnected && !s.AgentConnected
			s.mu.Unlock()
			if isStale || now.Sub(s.CreatedAt) > 24*time.Hour {
				om.history = append(om.history, s)
				if len(om.history) > 10 {
					om.history = om.history[len(om.history)-10:]
				}
				delete(om.sessions, id)
				// Clean up screenshot files
				sessionDir := filepath.Join(om.dataDir, "observe", id)
				if err := os.RemoveAll(sessionDir); err != nil {
					log.Printf("[observe] cleanup failed for session %s: %v", id, err)
				}
			}
		}
		om.mu.Unlock()
	}
}

// SaveScreenshot saves a base64-encoded screenshot for a session.
func (om *observeManager) SaveScreenshot(sessionID, dataURL string) (string, error) {
	sess := om.Get(sessionID)
	if sess == nil {
		return "", fmt.Errorf("session not found")
	}

	// Strip data URL prefix
	b64 := dataURL
	if idx := strings.Index(dataURL, ","); idx >= 0 {
		b64 = dataURL[idx+1:]
	}

	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}

	sess.mu.Lock()
	sess.ScreenshotCount++
	count := sess.ScreenshotCount
	sess.DataTransferred += int64(len(data))
	sess.LastActivity = time.Now()
	sess.mu.Unlock()

	filename := fmt.Sprintf("screenshot_%03d.png", count)
	path := filepath.Join(om.dataDir, "observe", sessionID, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}

	return path, nil
}

// ── HTTP Handlers ──

// HandleObservePage serves the observe mode landing page.
func (rl *Relay) HandleObservePage(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from path: /bridge/observe/{sessionId}
	path := strings.TrimPrefix(r.URL.Path, "/bridge/observe/")
	sessionID := strings.TrimSuffix(path, "/")

	if sessionID == "" {
		// No session ID — show a "create session" page or redirect to dashboard
		http.Redirect(w, r, "/dashboard", 302)
		return
	}

	sess := rl.observeSessions.Get(sessionID)
	if sess == nil {
		http.Error(w, "Observe session not found or expired", 404)
		return
	}

	// Serve the observe page HTML
	observeFile := rl.appDir + "/pages/observe/index.html"
	data, err := os.ReadFile(observeFile)
	if err != nil {
		http.Error(w, "Observe page not found", 500)
		return
	}

	html := string(data)
	html = strings.ReplaceAll(html, "__SESSION_ID__", sessionID)
	html = strings.ReplaceAll(html, "__SESSION_MODE__", sess.Mode)
	html = strings.ReplaceAll(html, "__SESSION_LABEL__", sess.Label)
	html = strings.ReplaceAll(html, "__SESSION_QUALITY__", sess.Quality)
	html = strings.ReplaceAll(html, "__SESSION_TAB_AUDIO__", fmt.Sprintf("%v", sess.TabAudio))
	html = strings.ReplaceAll(html, "__SESSION_SYS_AUDIO__", fmt.Sprintf("%v", sess.SysAudio))
	html = strings.ReplaceAll(html, "__SESSION_PERSISTENT__", fmt.Sprintf("%v", sess.Persistent))
	html = strings.ReplaceAll(html, "__SESSION_TIMEOUT__", fmt.Sprintf("%d", sess.Timeout))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write([]byte(html))
}

// HandleObserveAPI handles all /api/bridge/observe/ routes.
func (rl *Relay) HandleObserveAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/bridge/observe/")
	path = strings.TrimSuffix(path, "/")

	// POST /api/observe/sessions — create session
	if path == "sessions" && r.Method == "POST" {
		rl.handleCreateObserveSession(w, r)
		return
	}

	// GET /api/observe/sessions — list sessions
	if path == "sessions" && r.Method == "GET" {
		rl.handleListObserveSessions(w, r)
		return
	}

	// GET /api/observe/history — session history
	if path == "history" && r.Method == "GET" {
		rl.handleObserveHistory(w, r)
		return
	}

	// GET/POST /api/observe/settings — default settings
	if path == "settings" {
		rl.handleObserveSettings(w, r)
		return
	}

	// Routes with session ID: /api/observe/{id}/...
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 1 {
		http.Error(w, "Not found", 404)
		return
	}
	sessionID := parts[0]
	subPath := ""
	if len(parts) > 1 {
		subPath = parts[1]
	}

	sess := rl.observeSessions.Get(sessionID)
	if sess == nil {
		http.Error(w, "Session not found", 404)
		return
	}

	switch subPath {
	case "screenshot":
		rl.handleObserveScreenshot(w, r, sess)
	case "message":
		rl.handleObserveMessage(w, r, sess)
	case "connect":
		rl.handleObserveConnect(w, r, sess)
	case "status":
		rl.handleObserveStatus(w, r, sess)
	case "end":
		rl.handleObserveEnd(w, r, sess)
	default:
		http.Error(w, "Not found", 404)
	}
}

func (rl *Relay) handleCreateObserveSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode       string `json:"mode"`
		Timeout    int    `json:"timeout"`
		Label      string `json:"label"`
		SessionKey string `json:"sessionKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad request", 400)
		return
	}

	if body.Mode == "" {
		body.Mode = "text"
	}

	sess := rl.observeSessions.Create(body.Mode, body.Label, body.SessionKey, body.Timeout)

	// Build URL
	host := r.Host
	scheme := "https"
	if r.TLS == nil {
		if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		} else {
			scheme = "http"
		}
	}

	observeURL := fmt.Sprintf("%s://%s/bridge/observe/%s?mode=%s", scheme, host, sess.ID, sess.Mode)
	if body.Timeout > 0 {
		sess.Timeout = body.Timeout
	}

	// Build agent connection info
	agentInfo := map[string]interface{}{
		"session_id": sess.ID,
		"api": map[string]string{
			"screenshot": fmt.Sprintf("/api/bridge/observe/%s/screenshot", sess.ID),
			"message":    fmt.Sprintf("/api/bridge/observe/%s/message", sess.ID),
			"stream_ws":  fmt.Sprintf("/api/bridge/observe/%s/stream", sess.ID),
			"status":     fmt.Sprintf("/api/bridge/observe/%s/status", sess.ID),
			"end":        fmt.Sprintf("/api/bridge/observe/%s/end", sess.ID),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         sess.ID,
		"url":        observeURL,
		"created":    sess.CreatedAt.Format(time.RFC3339),
		"agent_info": agentInfo,
	})
	log.Printf("[observe] session created: %s mode=%s label=%q", sess.ID, sess.Mode, sess.Label)
}

func (rl *Relay) handleListObserveSessions(w http.ResponseWriter, r *http.Request) {
	sessions := rl.observeSessions.List()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"sessions": sessions})
}

func (rl *Relay) handleObserveHistory(w http.ResponseWriter, r *http.Request) {
	history := rl.observeSessions.History()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"history": history})
}

func (rl *Relay) handleObserveSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rl.observeSessions.settings)
		return
	}
	if r.Method == "POST" {
		var s observeSettings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			http.Error(w, "Bad request", 400)
			return
		}
		rl.observeSessions.settings = &s
		if err := rl.observeSessions.saveSettings(&s); err != nil {
			log.Printf("[observe] failed to save settings: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		return
	}
	http.Error(w, "Method not allowed", 405)
}

func (rl *Relay) handleObserveScreenshot(w http.ResponseWriter, r *http.Request, sess *ObserveSession) {
	if r.Method == "GET" {
		// Return latest screenshot
		dir := filepath.Join(rl.observeSessions.dataDir, "observe", sess.ID)
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) == 0 {
			http.Error(w, "No screenshots available", 404)
			return
		}
		// Find latest
		var latest string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "screenshot_") {
				latest = filepath.Join(dir, e.Name())
			}
		}
		if latest == "" {
			http.Error(w, "No screenshots available", 404)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		http.ServeFile(w, r, latest)
		return
	}
	http.Error(w, "Method not allowed", 405)
}

func (rl *Relay) handleObserveMessage(w http.ResponseWriter, r *http.Request, sess *ObserveSession) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var body struct {
		Text    string   `json:"text"`
		Buttons []string `json:"buttons,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad request", 400)
		return
	}

	sess.mu.Lock()
	sess.LatestMessage = body.Text
	sess.LastActivity = time.Now()
	sess.mu.Unlock()

	// Send to the user's browser via WebSocket
	msg := map[string]interface{}{
		"type": "agent_message",
		"text": body.Text,
		"ts":   time.Now().UnixMilli(),
	}
	if len(body.Buttons) > 0 {
		msg["buttons"] = body.Buttons
	}

	rl.observeWSClients.Broadcast(sess.ID, msg)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (rl *Relay) handleObserveConnect(w http.ResponseWriter, r *http.Request, sess *ObserveSession) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var body struct {
		SessionKey string `json:"sessionKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad request", 400)
		return
	}

	sess.mu.Lock()
	sess.TargetSessionKey = body.SessionKey
	sess.AgentConnected = true
	sess.LastActivity = time.Now()
	sess.mu.Unlock()

	// Notify user's browser
	rl.observeWSClients.Broadcast(sess.ID, map[string]interface{}{
		"type":   "agent_connected",
		"status": true,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	log.Printf("[observe] agent connected to session %s", sess.ID)
}

func (rl *Relay) handleObserveStatus(w http.ResponseWriter, r *http.Request, sess *ObserveSession) {
	sess.mu.Lock()
	status := map[string]interface{}{
		"id":               sess.ID,
		"mode":             sess.Mode,
		"label":            sess.Label,
		"user_connected":   sess.UserConnected,
		"agent_connected":  sess.AgentConnected,
		"data_transferred": sess.DataTransferred,
		"screenshot_count": sess.ScreenshotCount,
		"created_at":       sess.CreatedAt.Format(time.RFC3339),
		"last_activity":    sess.LastActivity.Format(time.RFC3339),
		"latest_message":   sess.LatestMessage,
	}
	sess.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (rl *Relay) handleObserveEnd(w http.ResponseWriter, r *http.Request, sess *ObserveSession) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	// Notify user's browser
	rl.observeWSClients.Broadcast(sess.ID, map[string]interface{}{
		"type": "session_ended",
	})

	rl.observeSessions.Remove(sess.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	log.Printf("[observe] session ended: %s", sess.ID)
}
