# ⚡ VelBridge — Agent Setup Guide

**For AI agents:** Step-by-step instructions to add VelBridge (browser relay) to a Vel dashboard.

---

> ## ⚠️ Prerequisites
>
> **Complete the [Vel Framework Setup](https://github.com/essdee/vel/blob/main/AGENT-SETUP.md) first.**
>
> Follow that guide through **Step 5 (test locally)** — it covers Go installation, cloning Vel, creating `config.json`, setting up `.env`, systemd, reverse proxy, and Telegram bot setup.
>
> **Come back here after your Vel instance is running.**

---

## Step 0 — Ask the user

Before starting, ask:

1. **OpenClaw relay config** — Do they want to configure OpenClaw to use this relay automatically?

---

## Step 1 — Install VelBridge

```bash
cd <install-dir>/apps/
git clone https://github.com/karthikeyan5/velbridge.git
```

Rebuild Vel to include VelBridge's server-side code:

```bash
cd <install-dir>
./vel build
```

This scans VelBridge's Go server code, generates imports, and compiles a single binary with the relay endpoints included.

---

## Step 2 — Configure

VelBridge works out of the box with no extra configuration. The relay endpoints are automatically registered at `/relay/*`.

To configure OpenClaw to use this relay, set the relay URL in your OpenClaw config:

```bash
# In your OpenClaw gateway config, set:
# relay_url: https://<domain>/relay
```

---

## Step 3 — Restart and verify

```bash
sudo systemctl restart vel

# Check relay is running
curl -s https://<domain>/relay/status

# Check CDP endpoint
curl -s https://<domain>/relay/cdp/json/version
```

---

## Step 4 — Connect a browser

1. Install the VelBridge Chrome extension
2. Click the extension icon on a tab
3. The extension will connect to `wss://<domain>/relay/ws`
4. Use pairing codes or direct token auth to establish the session

---

## Updating

```bash
cd <install-dir>/apps/velbridge
git pull
cd <install-dir>
./vel build
sudo systemctl restart vel
```

## Step 5 — Using VelBridge from your agent

VelBridge ships a Python helper library at `helpers/browser.py` that handles the relay protocol, Bezier mouse movement, and human-like interaction.

### Install dependency

```bash
pip install websockets
```

### Copy the helper to your workspace

```bash
cp <install-dir>/apps/velbridge/helpers/browser.py /tmp/browser.py
```

### Agent usage flow

```python
from browser import Browser

# 1. Connect to the relay
b = Browser(
    relay_token="<relay_token>",     # From pairing activation response
    server="https://<domain>",       # Your Vel instance URL
    human_mode=True,                 # Bezier mouse, random delays, jitter
)
b.connect()

# 2. ALWAYS open a new tab — never navigate existing tabs
tab = b.new_tab("https://www.google.com")

# 3. Interact naturally
b.type_text("Tirupur weather", "textarea[name=q]")
b.press_key("Enter")
b.wait_for_load()

# 4. Read results
print(b.get_title())
print(b.read_page())

# 5. Take screenshots
b.screenshot("/tmp/result.png")

# 6. Clean up — close YOUR tab, keep the bridge alive
b.close_tab(tab)
b.disconnect()
```

### Available methods

| Method | Description |
|---|---|
| `connect()` / `disconnect()` | Open/close relay WebSocket |
| `new_tab(url)` | Open a new tab (safe — never touches bridge) |
| `close_tab(id)` | Close a specific tab |
| `list_tabs()` | List open tabs `[{id, title, url}]` |
| `navigate(url)` | Navigate current tab |
| `click(x, y)` | Click with Bezier mouse movement |
| `click_element(selector)` | Find element, click its center |
| `type_text(text, selector?)` | Type with random per-key delays |
| `press_key(key)` | Enter, Tab, Escape, etc. |
| `scroll(direction, amount)` | up/down/left/right |
| `evaluate(js)` | Run JS, return result |
| `read_page()` | Get visible text |
| `read_html()` | Get full HTML |
| `screenshot(path?)` | Full page PNG |
| `screenshot_element(selector)` | Element-specific screenshot |

### Context manager (recommended)

```python
with Browser(relay_token="<token>", server="https://<domain>", human_mode=True) as b:
    b.new_tab("https://example.com")
    print(b.read_page())
# Auto-disconnects when done
```

---

## ⚠️ Critical: Never Navigate the Bridge Tab

**The bridge tab is the relay's lifeline.** It maintains the WebSocket connection between the user's browser and your server.

When you receive a target list from the relay, one of them will be the bridge tab itself (URL contains `/relay/bridge`). **Never navigate, close, or interact with this tab.** If you do, the WebSocket dies and the entire relay connection drops.

**Rules:**
- Filter out the bridge tab when choosing targets — skip any target whose URL contains `/relay/bridge`
- Only navigate/interact with the user's real tabs (other pages they have open)
- If you need a fresh tab, use CDP's `Target.createTarget` to open a new one — don't reuse the bridge

## Troubleshooting

- **WebSocket connection fails** → Ensure your reverse proxy has `Upgrade` and `Connection` headers set
- **Pairing code expired** → Codes expire after 5 minutes, request a new one
- **No targets showing** → Browser extension must be connected and have tabs open
- **CDP proxy not working** → Check that the relay token matches between browser and agent
- **Relay suddenly disconnects** → You probably navigated the bridge tab. Reconnect and don't touch it.
