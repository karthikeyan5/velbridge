package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// launcherInfo holds the CDP WS URL posted by a launcher script.
type launcherInfo struct {
	WSURL     string    `json:"wsUrl"`
	CreatedAt time.Time `json:"-"`
}

// launcherStore maps launcherId → CDP info, with 5-min TTL.
type launcherStore struct {
	mu    sync.RWMutex
	items map[string]*launcherInfo
}

func newLauncherStore() *launcherStore {
	s := &launcherStore{items: make(map[string]*launcherInfo)}
	go s.cleanup()
	return s
}

func (s *launcherStore) Set(id string, info *launcherInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info.CreatedAt = time.Now()
	s.items[id] = info
}

func (s *launcherStore) Get(id string) *launcherInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.items[id]
}

func (s *launcherStore) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for k, v := range s.items {
			if now.Sub(v.CreatedAt) > 5*time.Minute {
				delete(s.items, k)
			}
		}
		s.mu.Unlock()
	}
}

// HandleCDPInfo handles GET (bridge polls for WS URL) and POST (launcher sends WS URL).
// No auth required — launcher ID is the secret (64-bit random, 5-min TTL).
func (rl *Relay) HandleCDPInfo(w http.ResponseWriter, r *http.Request) {
	launcherID := r.URL.Query().Get("launcher")
	if launcherID == "" {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing launcher"})
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "POST":
		var body struct {
			WSURL string `json:"wsUrl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.WSURL == "" {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing wsUrl"})
			return
		}
		rl.launchers.Set(launcherID, &launcherInfo{WSURL: body.WSURL})
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})

	default: // GET
		info := rl.launchers.Get(launcherID)
		if info == nil {
			json.NewEncoder(w).Encode(map[string]string{"wsUrl": ""})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"wsUrl": info.WSURL})
	}
}
