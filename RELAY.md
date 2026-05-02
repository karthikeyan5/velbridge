# 🌐 VelBridge — Server Architecture

Technical documentation for VelBridge's server-side code: Debug relay, Proxy reverse proxy, Observe session manager, and Diff comparison.

---

## Overview

VelBridge registers all its routes via `vel.RegisterApp()` in `server/register.go`. All routes live under the unified `/bridge/` prefix. The core `Relay` struct holds state for all modes:

```go
type Relay struct {
    // Debug mode
    sessions  *SessionManager     // relay sessions (token → WS connections)
    pairing   *PairingManager     // 6-digit pairing codes
    launchers *launcherStore      // launcher ↔ bridge CDP info exchange

    // Proxy mode
    proxySessions  *proxyManager   // domain → cookie jar + session state
    proxyWSClients *proxyWSManager // browser WS connections for injected JS

    // Observe mode
    observeSessions  *observeManager   // session management + screenshot storage
    observeWSClients *observeWSManager // user + agent WS connections

    // App config
    appDir          string  // path to velbridge app directory
    openclawGateway string  // OpenClaw webhook URL
    openclawToken   string  // OpenClaw auth token
}
```

---

## Route Namespace

All routes unified under `/bridge/`:

```
/bridge/debug/                  ← Debug mode (CDP relay)
/bridge/proxy/{domain}/{path}   ← Proxy mode (reverse proxy)
/bridge/observe/{sessionId}     ← Observe mode (screen sharing)
/bridge/diff/?a=X&b=Y          ← Diff mode (visual comparison)
/bridge/status                  ← Combined status for all modes
/api/bridge/observe/            ← Observe REST/WS API
```

---

## Mode 1: Debug Relay (CDP)

### Architecture

```
┌──────────┐     WebSocket      ┌─────────────┐     WebSocket      ┌──────────┐
│  Browser  │ ◄──────────────► │  Vel Server  │ ◄──────────────► │  Agent   │
│ (bridge)  │                   │ (relay code) │                   │(OpenClaw)│
└──────────┘                   └─────────────┘                   └──────────┘
```

### Pairing Flow
1. Bridge requests code: `GET /bridge/debug/pair/new` → `{ code, token, expiresAt }`
2. Bridge displays code, polls `GET /bridge/debug/pair/status?token=<token>`
3. Agent activates: `POST /bridge/debug/pair/activate` (bot token auth) → `{ relayToken }`
4. Bridge receives token, connects WS to `/bridge/debug/ws?token=<relayToken>`
5. Agent connects to `/bridge/debug/cdp/ws?token=<relayToken>` or `/bridge/debug/cdp`

### Endpoints

| Endpoint | Auth | Description |
|----------|------|-------------|
| `/bridge/debug/token` | Cookie | Get relay token for authenticated user |
| `/bridge/debug/ws` | Token | Browser-side WebSocket |
| `/bridge/debug/cdp` | Token | Agent-side WebSocket (envelope format) |
| `/bridge/debug/json` | Token | Cached target list |
| `/bridge/debug/status` | Cookie | Relay status for dashboard panel |
| `/bridge/debug/bridge` | None | Bridge page |
| `/bridge/debug/download` | None | Launcher script download |
| `/bridge/debug/connect` | None | Pairing page |
| `/bridge/debug/pair/new` | None | Create pairing code |
| `/bridge/debug/pair/status` | Token | Poll pairing status |
| `/bridge/debug/pair/activate` | Bot token | Activate pairing code |
| `/bridge/debug/cdp/json/version` | Token | CDP `/json/version` |
| `/bridge/debug/cdp/json/list` | Token | CDP `/json/list` |
| `/bridge/debug/cdp/ws` | Token | Raw CDP WebSocket proxy |
| `/bridge/debug/cdp/status` | Token | Enhanced status + targets |
| `/bridge/debug/cdp-info` | Launcher ID | Launcher ↔ bridge coordination |

### Server Code
- `relay.go` — Core relay: WebSocket handlers, session token management
- `session.go` — `RelaySession` + `SessionManager`
- `pairing.go` — 6-char code generation, expiry, activation
- `pair_handlers.go` — HTTP handlers for pairing
- `cdp_proxy.go` — CDP-compatible proxy (raw JSON-RPC, no envelope)
- `launcher.go` — `launcherStore` for CDP info exchange
- `download.go` — Launcher script generation + bridge HTML page

---

## Mode 2: Proxy (Reverse Proxy)

### Architecture

```
┌──────────┐     HTTP          ┌─────────────┐     HTTP          ┌──────────┐
│ User     │ ←────────────── │  Vel Server  │ ──────────────→ │  Target  │
│ Browser  │ /bridge/proxy/  │  (proxy.go)  │  Reverse Proxy  │  Site    │
└──────────┘                  └─────────────┘                  └──────────┘
      ↑                              ↑
      │ WebSocket                    │ WebSocket
      ↓                              ↓
  Injected JS ←────────────→ /bridge/proxy/_ws
  (monitor.js, commands.js)
```

### How It Works

1. **Request arrives** at `/bridge/proxy/{domain}/{path}`
2. **Security check**: `isBlockedTarget()` resolves the domain via DNS, blocks if any IP is private/local/reserved
3. **`httputil.ReverseProxy`** forwards the request to `https://{domain}/{path}`:
   - `Director`: sets Host, Origin, Referer; attaches cookies from server-side jar
   - `ModifyResponse`: captures Set-Cookie → jar; strips CSP/CORS; rewrites URLs in HTML/CSS; injects monitoring JS
4. **HTML responses** get:
   - URL rewriting (absolute → `/bridge/proxy/{domain}/`, root-relative → `/bridge/proxy/{domain}/`)
   - `<meta>` CSP removal, `integrity` attribute stripping, `<base>` tag removal
   - JS injection: `html2canvas.min.js`, `monitor.js`, `commands.js`, `recorder.js`
5. **CSS responses** get URL rewriting (same pattern)
6. **Other content types** pass through unchanged

### IP Blocking (`isBlockedTarget`)
```go
func isBlockedTarget(domain string) bool {
    // Block: localhost, 0.0.0.0, [::1]
    // Resolve domain via net.LookupHost
    // Check each IP: IsLoopback, IsPrivate, IsLinkLocalUnicast,
    //   IsLinkLocalMulticast, IsUnspecified, 169.254.x.x
}
```

### Server-Side Cookie Jar
Each `ProxySession` holds a `CookieJar map[string][]*http.Cookie`. Cookies from `Set-Cookie` headers are captured and replayed on subsequent requests. This allows sessions to survive page navigation without browser cookies.

### WebSocket Proxy
Target sites using WebSockets are intercepted by the injected `monitor.js`:
- The native `WebSocket` constructor is replaced
- New connections are routed through `/bridge/proxy/_wsproxy?target={original_url}`
- The Go handler (`HandleProxyWSRelay`) creates a bidirectional WS relay
- Same `isBlockedTarget()` check applies to WS targets

### Endpoints

| Endpoint | Auth | Description |
|----------|------|-------------|
| `/bridge/proxy/{domain}/{path}` | None | Reverse proxy |
| `/bridge/proxy/_core/bridge/{file}` | None | Vendored JS files |
| `/bridge/proxy/_sessions` | Vel session/API key or bot token | List active sessions |
| `/bridge/proxy/_latest` | Vel session/API key or bot token | Resolve latest session for a domain |
| `/bridge/proxy/_ws` | Per-session control token (or Vel session/API key/bot token) | Browser-side WS (injected JS) |
| `/bridge/proxy/_wsproxy` | None | WS-to-WS relay |
| `/bridge/proxy/_agent` | Vel session/API key or bot token | Agent-side WS |
| `/bridge/proxy/_cookies` | Vel session/API key or bot token | Import cookies |

### Server Code
- `proxy.go` — `proxyManager`, `isBlockedTarget()`, `HandleProxy`, URL rewriting, JS injection
- `proxy_ws.go` — `proxyWSManager`, `HandleProxyWS`, `HandleProxyWSRelay`, `HandleProxyAgentWS`

### Injected JS (core/bridge/)
- `monitor.js` — Console/error/network capture, WebSocket interception, OAuth detection, WS connection
- `commands.js` — Screenshot (getDisplayMedia primary, html2canvas fallback), navigate, scroll, click, eval
- `recorder.js` — Interaction recording (click/input/scroll with multi-strategy element identification) and replay
- `html2canvas.min.js` — Vendored html2canvas library for fallback screenshots

---

## Mode 3: Observe (Screen Sharing + Chat)

### Architecture

```
┌──────────────┐       WebSocket          ┌──────────────┐       WebSocket        ┌──────────────┐
│ User Browser │ ←────────────────────── │  Vel Server  │ ──────────────────── │   Agent      │
│ observe page │  /bridge/observe/_ws    │ (observe.go) │  /api/bridge/       │  (OpenClaw)  │
│              │  screenshots, frames,   │              │  observe/{id}/      │              │
│              │  user messages           │              │  stream (WS)        │              │
└──────────────┘                         └──────────────┘                      └──────────────┘
                                                │
                                                │ POST /hooks/agent
                                                ↓
                                         ┌──────────────┐
                                         │  OpenClaw    │
                                         │  Gateway     │
                                         └──────────────┘
```

### Session Lifecycle
1. **Created** via `POST /api/bridge/observe/sessions` (agent-initiated) or from dashboard
2. **User connects** — opens `/bridge/observe/{id}` page, establishes WebSocket to `/bridge/observe/_ws`
3. **Agent connects** — via `POST /api/bridge/observe/{id}/connect` (REST) or WebSocket to `/api/bridge/observe/{id}/stream`
4. **Communication** — bidirectional text + screenshots/frames via WebSocket
5. **Ended** — via `POST /api/bridge/observe/{id}/end`, user closing page, or timeout
6. **Cleanup** — sessions without activity expire after `timeout` seconds (default 30)

### Endpoints

| Endpoint | Auth | Description |
|----------|------|-------------|
| `/bridge/observe/{id}` | None | Observe page (public) |
| `/bridge/observe/_ws` | Vel session or bot token | User browser WebSocket |
| `POST /api/bridge/observe/sessions` | API | Create session |
| `GET /api/bridge/observe/sessions` | API | List active sessions |
| `GET /api/bridge/observe/history` | API | Session history (last 10) |
| `GET/POST /api/bridge/observe/settings` | API | Default settings |
| `GET /api/bridge/observe/{id}/screenshot` | API | Latest screenshot |
| `POST /api/bridge/observe/{id}/message` | API | Send text to user |
| `WS /api/bridge/observe/{id}/stream` | API | Agent stream WebSocket |
| `POST /api/bridge/observe/{id}/connect` | API | Agent connection |
| `GET /api/bridge/observe/{id}/status` | API | Session status |
| `POST /api/bridge/observe/{id}/end` | API | End session |

### Server Code
- `observe.go` — `observeManager`, `ObserveSession`, HTTP handlers, screenshot storage
- `observe_ws.go` — `observeWSManager`, user WS handler, agent WS handler, OpenClaw webhook

---

## Mode 4: Diff (Visual Comparison)

`/bridge/diff/?a={url_a}&b={url_b}` serves `pages/diff/index.html`:
- Two iframes load the compared pages (typically proxied URLs)
- Three diff modes: overlay (adjustable opacity), swipe (drag handle), side-by-side
- DOM diff: structural comparison with color-coded highlighting
- Synchronized scrolling between both views

### Server Code
- Handled by `HandleDiffPage` in `proxy.go` — reads `pages/diff/index.html`, substitutes URL placeholders

---

## Combined Status Endpoint

`GET /bridge/status` stays public for health/availability checks, but unauthenticated requests receive a redacted payload (no active session/history details).
Use an authenticated Vel session or `Authorization: Bearer <api_key>` to get the full response:
```json
{
  "debug": { "state": "connected", "msgCount": 42 },
  "proxy_sessions": [{ "id": "...", "domain": "example.com", "since": "..." }],
  "observe_sessions": [{ "id": "...", "mode": "screenshot", "user_connected": true, ... }],
  "observe_history": [{ "id": "...", "label": "...", "mode": "...", "since": "..." }],
  "observe_settings": { "quality": "auto", "timeout": 30 }
}
```

---

## File Structure

```
server/
├── register.go        # vel.RegisterApp + route registration
├── relay.go           # Core Relay struct, debug WS handlers, combined status
├── session.go         # RelaySession + SessionManager
├── pairing.go         # 6-char pairing codes
├── pair_handlers.go   # Pairing HTTP handlers
├── cdp_proxy.go       # CDP-compatible proxy endpoints
├── launcher.go        # Launcher ↔ bridge coordination
├── download.go        # Launcher scripts + bridge HTML
├── proxy.go           # Reverse proxy, URL rewriting, cookie jar, diff handler
├── proxy_ws.go        # Proxy mode WebSocket (browser + agent + WS relay)
├── observe.go         # Observe session management + API handlers
└── observe_ws.go      # Observe WebSocket (user + agent + webhooks)

core/bridge/
├── monitor.js         # Console/error/network capture, OAuth detection
├── commands.js        # Screenshot, navigate, scroll, click, eval
├── recorder.js        # Interaction recording + hybrid replay
└── html2canvas.min.js # Vendored html2canvas

pages/
├── relay-connect/     # Debug mode pairing page
├── diff/index.html    # Visual diff (overlay/swipe/side-by-side)
└── observe/index.html # Observe mode (settings, chat, PiP)

panels/browser-relay/
├── manifest.json      # Panel config
└── ui.js              # Dashboard panel (3-tab: Debug/Proxy/Observe + Diff)
```
