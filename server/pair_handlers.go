package server

import (
	"encoding/json"
	"net/http"
	"strings"

	vel "vel/pkg/vel"
)

// HandlePairNew creates a new pairing code. No auth required.
func (rl *Relay) HandlePairNew(w http.ResponseWriter, r *http.Request) {
	code, token, expiresAt, err := rl.pairing.NewPairing()
	if err != nil {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":      code,
		"token":     token,
		"expiresAt": expiresAt,
	})
}

// HandlePairStatus checks if a pairing has been activated. No auth required (token is secret).
func (rl *Relay) HandlePairStatus(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing token"})
		return
	}
	activated, relayToken := rl.pairing.Status(token)
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{"activated": activated}
	if activated {
		resp["relayToken"] = relayToken
	}
	json.NewEncoder(w).Encode(resp)
}

// HandlePairActivate activates a pairing code. Requires bot token auth.
func (rl *Relay) HandlePairActivate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	// Check bot token auth
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing authorization"})
		return
	}
	botToken := strings.TrimPrefix(authHeader, "Bearer ")
	if !vel.CheckBotToken(botToken) {
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid bot token"})
		return
	}

	var body struct {
		Code   string `json:"code"`
		UserID int64  `json:"userId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid body"})
		return
	}
	if body.Code == "" || body.UserID == 0 {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing code or userId"})
		return
	}

	if !vel.IsAllowed(body.UserID) {
		w.WriteHeader(403)
		json.NewEncoder(w).Encode(map[string]string{"error": "user not allowed"})
		return
	}

	relayToken, err := rl.pairing.Activate(body.Code, body.UserID)
	if err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "relayToken": relayToken})
}
