package server

import (
	"net/http"

	vel "vel/pkg/vel"
)

func init() {
	vel.RegisterApp(vel.AppRegistration{
		Name:     "velreach",
		Register: Register,
	})
}

// Register registers all browser relay routes on the mux.
func Register(mux *http.ServeMux, cfg vel.AppConfig) {
	rl := New()

	// Core relay endpoints
	mux.HandleFunc("/relay/token", rl.HandleToken)
	mux.HandleFunc("/relay/json", rl.HandleTargets)
	mux.HandleFunc("/relay/ws", rl.HandleBrowserWS)
	mux.HandleFunc("/relay/cdp", rl.HandleAgentWS)
	mux.HandleFunc("/relay/download", rl.HandleDownload)
	mux.HandleFunc("/relay/bridge", rl.HandleBridge)
	mux.HandleFunc("/relay/status", rl.HandleStatus)

	// Pairing
	mux.HandleFunc("/relay/pair/new", rl.HandlePairNew)
	mux.HandleFunc("/relay/pair/status", rl.HandlePairStatus)
	mux.HandleFunc("/relay/pair/activate", rl.HandlePairActivate)

	// CDP-compatible endpoints
	mux.HandleFunc("/relay/cdp/json/version", rl.HandleCDPJsonVersion)
	mux.HandleFunc("/relay/cdp/json/list", rl.HandleCDPJsonList)
	mux.HandleFunc("/relay/cdp/ws", rl.HandleCDPProxyWS)
	mux.HandleFunc("/relay/cdp/status", rl.HandleCDPStatusJSON)

	// Launcher <-> bridge coordination
	mux.HandleFunc("/relay/cdp-info", rl.HandleCDPInfo)
}
