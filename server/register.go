package server

import (
	"net/http"
	"os"
	"path/filepath"

	vel "vel/pkg/vel"
)

func init() {
	vel.RegisterApp(vel.AppRegistration{
		Name:     "velbridge",
		Register: Register,
	})
}

// Register registers all VelBridge routes on the mux.
func Register(mux *http.ServeMux, cfg vel.AppConfig) {
	rl := NewFull(cfg.Dir)

	// Load OpenClaw config from environment or config file
	rl.loadOpenClawConfig(cfg.Dir)

	// ── Debug Mode ──
	mux.HandleFunc("/bridge/debug/token", rl.HandleToken)
	mux.HandleFunc("/bridge/debug/json", rl.HandleTargets)
	mux.HandleFunc("/bridge/debug/ws", rl.HandleBrowserWS)
	mux.HandleFunc("/bridge/debug/cdp", rl.HandleAgentWS)
	mux.HandleFunc("/bridge/debug/download", rl.HandleDownload)
	mux.HandleFunc("/bridge/debug/bridge", rl.HandleBridge)
	mux.HandleFunc("/bridge/debug/status", rl.HandleStatus)
	// Note: /bridge/debug/connect is registered via app.json routes (serves pages/relay-connect/)

	// Session-based connect (skip pairing if logged in)
	mux.HandleFunc("/bridge/debug/connect/session", rl.HandleConnectSession)

	// Pairing
	mux.HandleFunc("/bridge/debug/pair/new", rl.HandlePairNew)
	mux.HandleFunc("/bridge/debug/pair/status", rl.HandlePairStatus)
	mux.HandleFunc("/bridge/debug/pair/activate", rl.HandlePairActivate)

	// CDP-compatible endpoints
	mux.HandleFunc("/bridge/debug/cdp/json/version", rl.HandleCDPJsonVersion)
	mux.HandleFunc("/bridge/debug/cdp/json/list", rl.HandleCDPJsonList)
	mux.HandleFunc("/bridge/debug/cdp/ws", rl.HandleCDPProxyWS)
	mux.HandleFunc("/bridge/debug/cdp/status", rl.HandleCDPStatusJSON)

	// Launcher <-> bridge coordination
	mux.HandleFunc("/bridge/debug/cdp-info", rl.HandleCDPInfo)

	// ── Proxy Mode ──
	mux.HandleFunc("/bridge/proxy/", rl.HandleProxy)
	mux.HandleFunc("/bridge/proxy/_ws", rl.HandleProxyWS)
	mux.HandleFunc("/bridge/proxy/_wsproxy", rl.HandleProxyWSRelay)
	mux.HandleFunc("/bridge/proxy/_agent", rl.HandleProxyAgentWS)
	mux.HandleFunc("/bridge/proxy/_cookies", rl.HandleProxyCookieImport)

	// ── Observe Mode ──
	mux.HandleFunc("/bridge/observe/", rl.HandleObservePage)
	mux.HandleFunc("/bridge/observe/_ws", rl.HandleObserveUserWS)

	// Observe API — handles both REST and WebSocket (agent stream)
	mux.HandleFunc("/api/bridge/observe/", func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a WebSocket upgrade for the stream endpoint
		if r.Header.Get("Upgrade") == "websocket" {
			rl.HandleObserveAgentWS(w, r)
			return
		}
		rl.HandleObserveAPI(w, r)
	})

	// ── Diff Mode ──
	mux.HandleFunc("/bridge/diff/", rl.HandleDiffPage)

	// ── Combined status for dashboard panel ──
	mux.HandleFunc("/bridge/status", rl.HandleV2Status)
}

// loadOpenClawConfig reads OpenClaw gateway config for observe mode webhooks.
func (rl *Relay) loadOpenClawConfig(appDir string) {
	// Try environment variables first
	if gw := os.Getenv("OPENCLAW_GATEWAY"); gw != "" {
		rl.openclawGateway = gw
	}
	if token := os.Getenv("OPENCLAW_TOKEN"); token != "" {
		rl.openclawToken = token
	}

	// Try reading from a config file in the app dir
	if rl.openclawGateway == "" {
		configPath := filepath.Join(appDir, "config.json")
		if data, err := os.ReadFile(configPath); err == nil {
			var cfg struct {
				OpenClawGateway string `json:"openclaw_gateway"`
				OpenClawToken   string `json:"openclaw_token"`
			}
			if err := jsonUnmarshal(data, &cfg); err == nil {
				if cfg.OpenClawGateway != "" {
					rl.openclawGateway = cfg.OpenClawGateway
				}
				if cfg.OpenClawToken != "" {
					rl.openclawToken = cfg.OpenClawToken
				}
			}
		}
	}

	// Fallback to localhost
	if rl.openclawGateway == "" {
		rl.openclawGateway = "http://localhost:4800"
	}
}
