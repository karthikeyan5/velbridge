# ⚡ VelBridge — Agent Setup Guide

**For AI agents:** Complete guide to using VelBridge's four modes — Debug, Proxy, Observe, and Diff.

---

> ## ⚠️ Prerequisites
>
> **Complete the [Vel Framework Setup](https://github.com/essdee/vel/blob/main/AGENT-SETUP.md) first.**
>
> Follow that guide through Step 5 — it covers Go, Vel, config.json, systemd, and reverse proxy.
>
> **Come back here after your Vel instance is running.**

---

## Step 1 — Install VelBridge

```bash
cd <vel-apps-dir>/
git clone https://github.com/karthikeyan5/velbridge.git
cd <vel-dir>/
./vel build && sudo systemctl restart vel
```

Verify:
```bash
curl -s https://<domain>/bridge/debug/status     # Debug mode status
curl -s https://<domain>/bridge/proxy/_sessions   # Proxy mode sessions
curl -s https://<domain>/bridge/status            # Combined status (all modes)
```

---

## Route Namespace

All routes under `/bridge/`:
```
/bridge/debug/                  ← Debug mode (CDP relay)
/bridge/proxy/{domain}/{path}   ← Proxy mode (reverse proxy)
/bridge/observe/{sessionId}     ← Observe mode (screen sharing)
/bridge/diff/?a=X&b=Y          ← Diff mode (visual comparison)
/bridge/status                  ← Combined status
/api/bridge/observe/            ← Observe REST/WS API
```

---

## Mode 1: Debug (Full Browser Control via CDP)

### When to use
Deep automation: click, type, navigate, screenshot, read DOM — on the user's **real browser** with all their cookies and sessions.

### Connection flow

```python
from browser import Browser

# Connect via relay token (from pairing)
b = Browser(relay_token="<token>", server="https://<domain>", human_mode=True)
b.connect()

# Always open a new tab
tab = b.new_tab("https://example.com")

# Interact
b.type_text("search query", "input[name=q]")
b.press_key("Enter")
b.wait_for_load()

# Read + screenshot
print(b.read_page())
b.screenshot("/tmp/result.png")

# Clean up (close YOUR tab, not the bridge)
b.close_tab(tab)
b.disconnect()
```

### Pairing code activation (from agent)
```bash
curl -X POST https://<domain>/bridge/debug/pair/activate \
  -H "Authorization: Bearer <bot_token>" \
  -H "Content-Type: application/json" \
  -d '{"code": "A3K7MN", "userId": 85720317}'
```

### CDP-compatible endpoints
```
GET  /bridge/debug/cdp/json/version?token=<token>  → CDP /json/version
GET  /bridge/debug/cdp/json/list?token=<token>     → CDP /json/list with WS URLs
WS   /bridge/debug/cdp/ws?token=<token>            → Raw CDP WebSocket (no envelope)
```

### ⚠️ Never navigate the bridge tab
The bridge tab (URL contains `/bridge/debug/bridge`) maintains the relay connection. If you navigate or close it, everything disconnects. Filter it out of target lists.

---

## Mode 2: Proxy (Reverse Proxy with Monitoring)

### When to use
Visual QA, clone verification, site monitoring — load any website through your server with full console/network/error capture.

### Create a proxy link
```bash
# Simply construct the URL
PROXY_URL="https://<domain>/bridge/proxy/example.com/"

# Or use the dashboard panel to generate one
```

### Agent control via WebSocket
```javascript
// Connect to the proxy session's agent WS
const ws = new WebSocket('wss://<domain>/bridge/proxy/_agent?session=<sessionId>');

// Send commands to the proxied page
ws.send(JSON.stringify({ type: 'screenshot' }));
ws.send(JSON.stringify({ type: 'info' }));
ws.send(JSON.stringify({ type: 'eval', js: 'document.title' }));
ws.send(JSON.stringify({ type: 'navigate', url: 'https://other-site.com' }));
ws.send(JSON.stringify({ type: 'scroll', x: 0, y: 500 }));
ws.send(JSON.stringify({ type: 'click', x: 100, y: 200 }));

// Receive responses
ws.onmessage = (e) => {
  const msg = JSON.parse(e.data);
  // msg.type: 'screenshot', 'info', 'eval_result', 'console', 'error', 'net'
};
```

### Screenshot response
```json
{
  "type": "screenshot",
  "data": "data:image/png;base64,...",
  "method": "native",
  "surface": "browser"
}
```

### Visual diff
```
https://<domain>/bridge/diff/?a=/bridge/proxy/site-a.com/&b=/bridge/proxy/site-b.com/
```
Supports overlay (adjustable opacity), swipe (drag handle), and side-by-side comparison. DOM diff highlights structural differences.

### Recording + replay
```javascript
// Start recording user interactions
ws.send(JSON.stringify({ type: 'start_recording' }));

// ... user interacts with page ...

// Stop recording — returns event list
ws.send(JSON.stringify({ type: 'stop_recording' }));

// Replay recording on another page
ws.send(JSON.stringify({ type: 'replay', data: recordedEvents }));
```

### Cookie import (OAuth flows)
```bash
curl -X POST https://<domain>/bridge/proxy/_cookies \
  -H "Content-Type: application/json" \
  -d '{"domain": "example.com", "cookies": "session_id=abc123\ncsrf_token=xyz789"}'
```

---

## Mode 3: Observe (Screen Sharing + Text Chat)

### When to use
Live user assistance, bug reports, guided walkthroughs — see the user's screen and communicate via text.

### Create an observe session

```bash
# Create session via API
curl -X POST https://<domain>/api/bridge/observe/sessions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <api_key>" \
  -d '{
    "mode": "screenshot",
    "label": "Router setup help",
    "timeout": 300,
    "sessionKey": "telegram:85720317"
  }'
```

Response:
```json
{
  "id": "abc123def456",
  "url": "https://<domain>/bridge/observe/abc123def456?mode=screenshot",
  "created": "2026-03-14T10:30:00Z",
  "agent_info": {
    "session_id": "abc123def456",
    "api": {
      "screenshot": "/api/bridge/observe/abc123def456/screenshot",
      "message": "/api/bridge/observe/abc123def456/message",
      "stream_ws": "/api/bridge/observe/abc123def456/stream",
      "status": "/api/bridge/observe/abc123def456/status",
      "end": "/api/bridge/observe/abc123def456/end"
    }
  }
}
```

### Send the link to the user
Send the `url` field to the user via Telegram or any channel.

### Link parameters
```
/bridge/observe/{id}?mode=text          → text-only (default)
/bridge/observe/{id}?mode=screenshot    → opens with screenshot prompt
/bridge/observe/{id}?mode=stream&fps=2  → asks to start streaming
/bridge/observe/{id}?autostart=true     → skips landing, auto-triggers share
```

### Connect as agent via WebSocket
```javascript
const ws = new WebSocket('wss://<domain>/api/bridge/observe/abc123def456/stream');

// Receive events from user
ws.onmessage = (e) => {
  const msg = JSON.parse(e.data);
  switch (msg.type) {
    case 'user_connected':    // User opened the observe page
    case 'screenshot':        // { path, method, ts }
    case 'frame':             // { data (base64), ts } — live stream frames
    case 'user_message':      // { text, ts }
    case 'stream_started':
    case 'stream_stopped':
  }
};

// Send messages to user
ws.send(JSON.stringify({
  type: 'agent_message',
  text: 'Click the gear icon in the top right',
  buttons: ['Done', 'I don\'t see it', 'Help']
}));

// Request a screenshot
ws.send(JSON.stringify({ type: 'request_screenshot' }));

// End session
ws.send(JSON.stringify({ type: 'end_session' }));
```

### REST API alternatives
```bash
# Send message to user
curl -X POST https://<domain>/api/bridge/observe/abc123/message \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <api_key>" \
  -d '{"text": "Click Settings", "buttons": ["Done", "Help"]}'

# Get latest screenshot
curl https://<domain>/api/bridge/observe/abc123/screenshot \
  -H "Authorization: Bearer <api_key>" \
  -o screenshot.png

# Check session status
curl https://<domain>/api/bridge/observe/abc123/status \
  -H "Authorization: Bearer <api_key>"

# End session
curl -X POST https://<domain>/api/bridge/observe/abc123/end \
  -H "Authorization: Bearer <api_key>"

# Connect agent (set sessionKey)
curl -X POST https://<domain>/api/bridge/observe/abc123/connect \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <api_key>" \
  -d '{"sessionKey": "telegram:85720317"}'
```

### Observe settings
```bash
# Get saved defaults
curl https://<domain>/api/bridge/observe/settings \
  -H "Authorization: Bearer <api_key>"

# Save defaults
curl -X POST https://<domain>/api/bridge/observe/settings \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <api_key>" \
  -d '{"quality":"auto","tab_audio":false,"sys_audio":false,"persistent":false,"timeout":30}'
```

### Quality presets
| Preset | FPS | Resolution | Use case |
|---|---|---|---|
| Auto | 2 | 720p | Default — adapts to bandwidth |
| Low | 1 | 480p | Slow connections |
| High | 5 | 1080p | Fast connections, detailed work |
| Screenshots Only | — | — | Manual 📸 button only |

### Agent-initiated workflow (typical)
```python
# 1. Create observe session
resp = requests.post(f'{base}/api/bridge/observe/sessions', json={
    'mode': 'screenshot',
    'label': 'Help with router settings',
    'sessionKey': 'telegram:85720317'
})
session = resp.json()

# 2. Send link to user on Telegram
send_message(user_id, f"I need to see your screen to help. Open this link:\n{session['url']}")

# 3. Connect via WebSocket and wait for screenshots
ws = websocket.connect(f'{ws_base}/api/bridge/observe/{session["id"]}/stream')
for msg in ws:
    data = json.loads(msg)
    if data['type'] == 'screenshot':
        analysis = analyze_image(data['path'])
        ws.send(json.dumps({'type': 'agent_message', 'text': analysis}))
    elif data['type'] == 'user_message':
        handle_response(data['text'])
```

---

## Mode 4: Diff (Visual Comparison)

### When to use
Staging vs production, clone verification, before/after deployment, design mockup vs implementation.

### Create a diff
```
https://<domain>/bridge/diff/?a=https://staging.example.com&b=https://example.com
```

Or with proxied URLs:
```
https://<domain>/bridge/diff/?a=/bridge/proxy/staging.example.com/&b=/bridge/proxy/example.com/
```

### Diff modes
- **Overlay** — Stack pages with opacity slider
- **Swipe** — Drag a handle to reveal before/after
- **Side by Side** — Split view with synchronized scrolling
- **DOM Diff** — Automated highlighting of structural differences

### Agent control via WebSocket
The diff page connects to `/bridge/proxy/_ws?session=diff` for agent commands:
```javascript
ws.send(JSON.stringify({ type: 'set_mode', mode: 'overlay' }));
ws.send(JSON.stringify({ type: 'set_opacity', value: 75 }));
ws.send(JSON.stringify({ type: 'dom_diff' }));
ws.send(JSON.stringify({ type: 'screenshot' }));
ws.send(JSON.stringify({ type: 'scroll', x: 0, y: 500 }));
```

---

## Combined Status API

```bash
curl https://<domain>/bridge/status -H "Authorization: Bearer <api_key>"
```

Returns:
```json
{
  "debug": { "state": "connected", "msgCount": 42 },
  "proxy_sessions": [{ "id": "...", "domain": "example.com", "since": "..." }],
  "observe_sessions": [{ "id": "...", "mode": "screenshot", "user_connected": true, ... }],
  "observe_history": [...],
  "observe_settings": { "quality": "auto", "timeout": 30 }
}
```

---

## Updating

```bash
cd <vel-apps-dir>/velbridge && git pull
cd <vel-dir>/ && ./vel build && sudo systemctl restart vel
```

---

## Troubleshooting

**WebSocket fails** → Check reverse proxy has `Upgrade` + `Connection` headers set
**Pairing expired** → Codes expire in 5 minutes; request a new one
**Proxy returns 403** → Target domain resolves to a private IP (blocked for security)
**Proxy CSS/images broken** → Some sites use unusual URL patterns; check URL rewriting
**Observe "No screenshots"** → User hasn't shared screen yet; they need to click the 📸 button
**Agent webhook not firing** → Set `OPENCLAW_GATEWAY` and `OPENCLAW_TOKEN` env vars or create `config.json` in the app directory
