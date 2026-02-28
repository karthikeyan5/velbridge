package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math/big"
	"sync"
	"time"
)

const (
	pairingCodeLen    = 6
	pairingExpiry     = 5 * time.Minute
	maxPendingPairs   = 20
	cleanupInterval   = 30 * time.Second
)

// Safe alphabet: no 0/O/1/I/L
var safeChars = []byte("ABCDEFGHJKMNPQRSTUVWXYZ23456789")

type pendingPairing struct {
	Code      string
	Token     string // pairing token for polling
	ExpiresAt time.Time
	Activated bool
	RelayToken string // set after activation
}

// PairingManager handles pairing code creation and activation.
type PairingManager struct {
	mu       sync.Mutex
	byCode   map[string]*pendingPairing
	byToken  map[string]*pendingPairing
	sessions *SessionManager
}

// NewPairingManager creates a new PairingManager and starts cleanup.
func NewPairingManager(sessions *SessionManager) *PairingManager {
	pm := &PairingManager{
		byCode:   make(map[string]*pendingPairing),
		byToken:  make(map[string]*pendingPairing),
		sessions: sessions,
	}
	go pm.cleanup()
	return pm
}

func generatePairingCode() string {
	code := make([]byte, pairingCodeLen)
	for i := range code {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(safeChars))))
		code[i] = safeChars[n.Int64()]
	}
	return string(code)
}

func generatePairingToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// NewPairing creates a new pairing code and token.
func (pm *PairingManager) NewPairing() (code, token string, expiresAt time.Time, err error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Count pending (non-expired)
	now := time.Now()
	count := 0
	for _, p := range pm.byCode {
		if !p.Activated && p.ExpiresAt.After(now) {
			count++
		}
	}
	if count >= maxPendingPairs {
		return "", "", time.Time{}, errors.New("too many pending pairings")
	}

	code = generatePairingCode()
	// Ensure unique
	for pm.byCode[code] != nil {
		code = generatePairingCode()
	}

	token = generatePairingToken()
	expiresAt = now.Add(pairingExpiry)

	p := &pendingPairing{
		Code:      code,
		Token:     token,
		ExpiresAt: expiresAt,
	}
	pm.byCode[code] = p
	pm.byToken[token] = p

	return code, token, expiresAt, nil
}

// Activate activates a pairing code for a user, creating a relay session.
func (pm *PairingManager) Activate(code string, userID int64) (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	p, ok := pm.byCode[code]
	if !ok {
		return "", errors.New("invalid pairing code")
	}
	if time.Now().After(p.ExpiresAt) {
		delete(pm.byCode, code)
		delete(pm.byToken, p.Token)
		return "", errors.New("pairing code expired")
	}
	if p.Activated {
		return "", errors.New("pairing code already used")
	}

	// Create relay session
	relayToken := pm.sessions.GetOrCreateToken(userID)

	p.Activated = true
	p.RelayToken = relayToken

	return relayToken, nil
}

// Status checks whether a pairing token has been activated.
func (pm *PairingManager) Status(pairingToken string) (activated bool, relayToken string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	p, ok := pm.byToken[pairingToken]
	if !ok {
		return false, ""
	}
	return p.Activated, p.RelayToken
}

func (pm *PairingManager) cleanup() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		pm.mu.Lock()
		now := time.Now()
		for code, p := range pm.byCode {
			// Remove expired non-activated, or activated older than 10 min
			if now.After(p.ExpiresAt) && !p.Activated {
				delete(pm.byCode, code)
				delete(pm.byToken, p.Token)
			} else if p.Activated && now.After(p.ExpiresAt.Add(5*time.Minute)) {
				delete(pm.byCode, code)
				delete(pm.byToken, p.Token)
			}
		}
		pm.mu.Unlock()
	}
}
