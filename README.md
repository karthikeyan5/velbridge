<p align="center">
  <strong>🌉 VelBridge</strong>
</p>

<h1 align="center">Four ways your agent controls any browser.</h1>

<p align="center">
  <strong>Debug</strong> — Full CDP control of a user's real browser<br>
  <strong>Proxy</strong> — Reverse-proxy any site with injected monitoring<br>
  <strong>Observe</strong> — See a user's screen and guide them via text<br>
  <strong>Diff</strong> — Compare any two pages instantly
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-2.0.0-c9a84c?style=flat-square" alt="Version">
  <img src="https://img.shields.io/badge/protocol-CDP%20%7C%20HTTP%20%7C%20WebSocket-blue?style=flat-square" alt="Protocol">
  <img src="https://img.shields.io/badge/modes-4-22c55e?style=flat-square" alt="Modes">
  <img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="License">
</p>

---

## Unified Route Namespace

All routes live under `/bridge/`:
```
/bridge/debug/      ← Full browser control via CDP
/bridge/proxy/      ← Reverse-proxy any site
/bridge/observe/    ← See user's screen + guide them
/bridge/diff/       ← Compare any two pages
```

---

## Four Modes at a Glance

| | 🛠 Debug | 🔀 Proxy | 👁️ Observe | 🔍 Diff |
|---|---|---|---|---|
| **What it does** | Full browser automation via CDP | Reverse-proxy any site through your server | See user's screen + text chat | Side-by-side visual comparison |
| **Setup** | Chrome extension + pairing code | Open a link | Open a link, click "Share" | Two URLs |
| **Agent sees** | CDP data (DOM, network, console) | Proxied page (with injected JS) | User's actual screen | Both pages overlaid |
| **Agent control** | Full (click, type, navigate) | Injected JS (screenshot, eval) | View-only + text messages | Mode switching, DOM diff |
| **Use case** | Deep debugging, automation | Visual QA, clone verification | Live assistance, bug reports | Staging vs production, A/B testing |
| **Mobile support** | ❌ | ✅ | Text-only ✅, streaming ❌ | ✅ (side-by-side) |

---

## Quick Start

### Install

```bash
# Clone into your Vel apps directory
cd /path/to/vel-apps/
git clone https://github.com/karthikeyan5/velbridge.git

# Rebuild Vel to include VelBridge
cd /path/to/vel/
./vel build
sudo systemctl restart vel
```

### Debug Mode (Chrome Extension)

1. Download the launcher from your dashboard's VelBridge panel
2. Run the launcher — it opens Chrome with CDP enabled
3. Send the 6-digit pairing code to your agent
4. Done! Agent can now control your browser

### Proxy Mode (Instant)

Open any site through your dashboard:
```
https://your-dashboard/bridge/proxy/example.com/
```

The page loads through your server with:
- Console/error/network monitoring injected
- URL rewriting for same-origin navigation
- Server-side cookie jar (survives page reloads)
- Screenshot capability (native or html2canvas fallback)
- WebSocket relay for real-time sites

### Observe Mode (Screen Sharing)

1. Agent creates a session: `POST /api/bridge/observe/sessions`
2. Agent sends the link to the user
3. User opens the link and shares their screen
4. Agent sees the screen and sends text instructions
5. User responds with quick buttons (Yes/No/Done)

Observe mode has three progressive states:
- **Text-only** — zero bandwidth, just chat
- **Screenshots** — user clicks 📸 to send a screenshot
- **Live stream** — real-time screen feed at configurable FPS

### Diff Mode (Instant Comparison)

Compare any two pages:
```
https://your-dashboard/bridge/diff/?a=https://site-a.com&b=https://site-b.com
```

Three diff modes: overlay (adjustable opacity), swipe (drag handle), side-by-side with synchronized scrolling. Plus automated DOM diff highlighting.

---

## Architecture

```
                          ┌─────────────────────────────────────────┐
                          │           Vel Server (Go)               │
                          │                                         │
┌──────────┐  WebSocket   │  ┌───────────┐  ┌───────────┐          │  REST/WS   ┌──────────┐
│ Browser  │◄────────────►│  │  Debug    │  │  Proxy    │          │◄──────────►│  Agent   │
│ (User)   │              │  │  Relay    │  │  Reverse  │          │            │(OpenClaw)│
└──────────┘              │  │           │  │  Proxy    │          │            └──────────┘
                          │  └───────────┘  └───────────┘          │
┌──────────┐  HTTP/WS     │  ┌───────────┐  ┌───────────┐         │
│ Browser  │◄────────────►│  │  Observe  │  │   Diff    │         │
│ (Proxy)  │              │  │  Session  │  │  Overlay  │         │
└──────────┘              │  │  Manager  │  │           │         │
                          │  └───────────┘  └───────────┘         │
                          └─────────────────────────────────────────┘
```

### Debug Mode Flow
```
Agent → /bridge/debug/cdp (WS) → Vel Relay → /bridge/debug/ws (WS) → Chrome (CDP) → Your tabs
```

### Proxy Mode Flow
```
Agent → /bridge/proxy/_agent (WS) → Vel Proxy → HTTP → Target site
                                              ↕
                                     URL rewriting + JS injection
                                              ↕
User → /bridge/proxy/{domain}/ (HTTP) → Proxied page with monitoring
```

### Observe Mode Flow
```
Agent → /api/bridge/observe/{id}/stream (WS) → Vel Observe ← /bridge/observe/_ws (WS) ← User's browser
         POST /api/bridge/observe/{id}/message →            → agent_message → User sees text
         GET /api/bridge/observe/{id}/screenshot →          ← screenshot data ← User clicks 📸
```

---

## Features

### 🛠 Debug Mode — Full control of a real browser
- 🔑 **Pairing code flow** — 6-digit code, no API keys needed
- 🖱️ **Human-like interaction** — Bézier mouse curves, typing delays
- 🔒 **Secure** — WebSocket relay, zero browser data stored
- 📡 **Cross-network** — Agent on VPS, browser on laptop
- 🖥️ **CDP-compatible** — Works with any CDP client

**Use cases:** Complex web app debugging, automated form filling, bot-resistant automation, e-commerce operations, social media management, corporate VPN access, web scraping, testing & QA.

### 🔀 Proxy Mode — Any site through your server
- 🔀 **Reverse proxy** — Load any site through your server
- 📝 **Console/error capture** — Real-time JS console + error monitoring
- 🌐 **Network capture** — XHR/fetch interception + timing
- 📸 **Screenshots** — `getDisplayMedia` (native) or `html2canvas` (fallback)
- 🍪 **Server-side cookies** — Cookie jar survives reloads
- 🔄 **URL rewriting** — Absolute and relative URLs rewritten
- 🔒 **OAuth detection** — Shows overlay for external auth flows
- 📡 **WebSocket relay** — Proxies WebSocket connections too
- 🔴 **Recording + replay** — Record interactions, replay with hybrid matching

**Use cases:** Website cloning verification, visual QA for deployments, mobile rendering verification, console & error monitoring, network request auditing, cross-browser testing, interaction recording, form testing, accessibility audits, client demos.

### 👁️ Observe Mode — See what your user sees
- 👁️ **Screen sharing** — See user's actual screen
- 💬 **Text chat** — Agent sends instructions, user responds
- 📸 **Screenshot on demand** — User clicks button to capture
- 📹 **Live streaming** — Configurable FPS (1-5fps)
- 🔊 **Audio support** — Tab and system audio via getDisplayMedia
- ⚙️ **Quality presets** — Auto / Low / High / Screenshots Only
- 🖼️ **PiP overlay** — Floating widget when user navigates away
- 🔗 **Webhook integration** — Auto-wakes agent via OpenClaw hooks

**Use cases:** Live tech support, user onboarding, bug reporting, workflow training, remote troubleshooting, quality review, pair programming, low-bandwidth assistance, desktop app support, customer success.

### 🔍 Diff — Compare any two pages instantly
- **Overlay** — Stack pages with opacity slider
- **Swipe** — Drag a handle to reveal before/after
- **Side by Side** — Split view with synchronized scrolling
- **DOM Diff** — Automated highlighting of structural differences (green=added, red=removed, yellow=changed)

**Use cases:** Staging vs production, design mockup vs implementation, before/after verification, clone verification, competitor monitoring, multi-locale comparison, responsive testing, CSS/theme impact, A/B test verification, accessibility checks.

---

## Dashboard Panel

The VelBridge panel on your Vel dashboard shows all four modes in a tabbed layout:

- **Debug tab** — Connection status, paired browser info, open tabs
- **Proxy tab** — Quick-create proxy URL, active proxy sessions
- **Observe tab** — Create observe links, active sessions, history, settings
- **Diff section** — Quick diff URL builder

---

## Why VelBridge?

**No other tool gives an AI agent four ways to see and control a browser.**

- Playwright/Puppeteer: headless only, detectable, no real user sessions
- Chrome DevTools: local only, no remote access
- Screen sharing apps: human-to-human, not agent-to-human
- Browser extensions: per-browser, complex setup

VelBridge is the only tool where an agent can:
- Control a user's real browser (Debug)
- Proxy any site with monitoring (Proxy)
- See a user's screen and guide them (Observe)
- Compare any two pages (Diff)

All from a single install. All through a web browser. No complex setup.

---

## Security

- **Proxy mode blocks private IPs** — `localhost`, `127.x`, `10.x`, `172.16-31.x`, `192.168.x`, `169.254.x` all blocked via DNS resolution checks
- **No data stored** — Debug mode is pure pass-through; proxy mode strips CSP but doesn't store content
- **Auth required** — API endpoints require authentication; only page-level URLs are public
- **WebSocket relay validates targets** — WS proxy rejects connections to private networks
- **Cookie jar is server-side** — Cookies from proxied sites never reach the user's browser

---

## Built on Vel

VelBridge is a [Vel](https://github.com/essdee/vel) app. It registers routes, a dashboard panel, and Go server code — all compiled into Vel's single binary.

---

<p align="center">
  <sub>Built on <a href="https://github.com/essdee/vel">⚡ Vel</a> — the framework where AI builds and the framework guarantees.</sub>
</p>
