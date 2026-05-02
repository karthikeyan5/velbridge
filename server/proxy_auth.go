package server

import (
	"net/http"
	"strings"

	vel "vel/pkg/vel"
)

func proxyControlAuthorized(r *http.Request) bool {
	if vel.Check(r) != nil {
		return true
	}
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	return token != "" && vel.CheckBotToken(token)
}

func (rl *Relay) proxySessionTokenAuthorized(r *http.Request, sessionID string) bool {
	if proxyControlAuthorized(r) {
		return true
	}
	sess := rl.proxySessions.GetByID(sessionID)
	if sess == nil || sess.ControlToken == "" {
		return false
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("X-Velbridge-Proxy-Token")
	}
	return token != "" && token == sess.ControlToken
}
