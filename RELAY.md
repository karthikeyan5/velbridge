# 🌐 Browser Relay

The `server/` directory contains Go server code that implements a **browser relay** — allowing OpenClaw agents to remotely control a user's browser via Chrome DevTools Protocol (CDP).

---

## Architecture

```
┌──────────┐     WebSocket      ┌─────────────┐     WebSocket      ┌──────────┐
│  Browser  │ ◄──────────────► │  Vel Server  │ ◄──────────────► │  Agent   │
│ (bridge)  │                   │ (relay code) │                   │(OpenClaw)│
└──────────┘                   └─────────────┘                   └──────────┘
```

- **Bridge**: JavaScript running in the browser that connects to the relay and forwards CDP commands
- **Relay**: Go server code (compiled into Vel binary) that routes messages between browser and agent
- **Agent**: OpenClaw connects via WebSocket or CDP-compatible endpoints

---

## Pairing Flow

1. Bridge page requests a pairing code (`/relay/pair/new`) — no auth required
2. Bridge displays a 6-character code (e.g., `A3K7MN`)
3. User tells their agent the code
4. Agent activates the code via `/relay/pair/activate` (bot token auth)
5. Bridge polls `/relay/pair/status` and receives a relay token
6. Bridge connects WebSocket to `/relay/ws?token=<relay_token>`
7. Agent connects to `/relay/cdp?token=<relay_token>` or uses CDP-compatible endpoints

---

## Endpoints

| Endpoint | Auth | Description |
|----------|------|-------------|
| `/relay/token` | Cookie | Get relay token for authenticated user |
| `/relay/ws` | Token | Browser-side WebSocket |
| `/relay/cdp` | Token | Agent-side WebSocket (envelope format) |
| `/relay/json` | Token | Cached target list |
| `/relay/status` | Cookie | Relay status for dashboard panel |
| `/relay/bridge` | None | Bridge page (served over HTTPS) |
| `/relay/download` | None | Launcher script download |
| `/relay/pair/new` | None | Create pairing code |
| `/relay/pair/status` | Token | Poll pairing status |
| `/relay/pair/activate` | Bot token | Activate pairing code |
| `/relay/connect` | None | Pairing page |
| **CDP-compatible** | | |
| `/relay/cdp/json/version` | Token | CDP `/json/version` equivalent |
| `/relay/cdp/json/list` | Token | CDP `/json/list` with per-target WS URLs |
| `/relay/cdp/ws` | Token | Raw CDP WebSocket proxy (no envelope) |
| `/relay/cdp/status` | Token | Enhanced status with target list |
| `/relay/cdp-info` | Launcher ID | Launcher↔bridge CDP info exchange |

---

## CDP-Compatible Proxy

The `/relay/cdp/*` endpoints speak standard CDP JSON-RPC (no envelope wrapping), making the relay compatible with tools that expect a direct CDP connection. The proxy:

- Responds to `Target.getTargets` from cached target list
- Translates `Target.attachToTarget` into relay connect envelopes
- Forwards all other CDP messages transparently

---

## Launcher Scripts

Launcher scripts (~30 lines) launch Chrome with `--remote-debugging-port` and `--remote-allow-origins` restricted to the server domain (not wildcard), then POST the CDP WebSocket URL to `/relay/cdp-info`. The bridge page polls this endpoint to discover the local Chrome instance.

---

## Server Code Structure

```
server/
├── register.go       # vel.RegisterApp init
├── relay.go          # Core relay (WebSocket, handlers)
├── session.go        # Session management
├── pairing.go        # Pairing code logic
├── pair_handlers.go  # Pairing HTTP handlers
├── cdp_proxy.go      # CDP-compatible proxy endpoints
├── launcher.go       # Launcher↔bridge coordination
└── download.go       # Launcher script download
```
