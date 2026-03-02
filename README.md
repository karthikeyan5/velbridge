# ⚡ VelBrowser — Browser Relay for OpenClaw Agents

VelBrowser is a [Vel](https://github.com/essdee/vel) app that gives OpenClaw agents browser control capabilities. It acts as a WebSocket relay between Chrome running on a user's machine and an AI agent running on a server.

## How It Works

```
┌──────────────┐    WebSocket     ┌──────────────┐    WebSocket/CDP    ┌──────────────┐
│  Chrome +     │ ◄──────────────► │  VelBrowser     │ ◄────────────────► │  OpenClaw     │
│  Extension    │   /relay/ws      │  Server       │   /relay/cdp       │  Agent        │
└──────────────┘                  └──────────────┘                     └──────────────┘
   User's PC                         Server                              Server
```

1. **User** opens Chrome with the OpenClaw Browser Relay extension
2. **Extension** connects to VelBrowser via WebSocket (`/relay/ws`)
3. **Agent** connects to VelBrowser via CDP-compatible WebSocket (`/relay/cdp`)
4. VelBrowser **relays** Chrome DevTools Protocol messages between them

The agent can then inspect tabs, run JavaScript, take screenshots, click elements — anything CDP supports.

## Pairing Flow

VelBrowser uses pairing codes to securely connect browser sessions:

1. Agent requests a pairing code → `POST /relay/pair/new` → returns `{code, token, expiresAt}`
2. User enters the 6-character code in the browser extension
3. Extension activates the pairing → `POST /relay/pair/activate`
4. Agent polls for activation → `GET /relay/pair/status?token=...` → gets `relayToken`
5. Both sides use the relay token for subsequent WebSocket connections

Pairing codes expire after 5 minutes. The alphabet excludes ambiguous characters (0/O/1/I/L).

## Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /relay/status` | Relay status (active sessions, etc.) |
| `GET /relay/token` | Token management |
| `GET /relay/json` | List available targets (tabs) |
| `WS /relay/ws` | Browser extension WebSocket |
| `WS /relay/cdp` | Agent WebSocket (envelope mode) |
| `WS /relay/cdp/ws` | Agent WebSocket (raw CDP mode) |
| `GET /relay/cdp/json/version` | CDP-compatible version endpoint |
| `GET /relay/cdp/json/list` | CDP-compatible target list |
| `POST /relay/pair/new` | Create pairing code |
| `GET /relay/pair/status` | Check pairing status |
| `POST /relay/pair/activate` | Activate pairing with code |
| `POST /relay/download` | File download via browser |
| `WS /relay/bridge` | Launcher bridge WebSocket |

## Architecture

- **SessionManager** — manages relay sessions keyed by token, tracks connected browser/agent WebSockets and CDP targets
- **PairingManager** — handles pairing code generation, activation, and cleanup (max 20 pending, 30s cleanup interval)
- **LauncherStore** — coordinates with launcher scripts that provide CDP WebSocket URLs (5-min TTL)
- **Relay** — main struct tying it all together, handles all HTTP/WebSocket handlers

## Panel

VelBrowser includes a `browser-relay` panel that shows relay status in the Vel dashboard.

## See Also

- **[Velboard](https://github.com/karthikeyan5/velboard)** — Real-time monitoring dashboard for your agent. CPU, memory, Claude usage, cron jobs, and more. Another Vel app.
