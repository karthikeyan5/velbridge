# ⚡ VelBrowser — Agent Setup Guide

**For AI agents:** Step-by-step instructions to set up VelBrowser (browser relay) from scratch.

---

## Step 0 — Ask the user

1. **Server domain** — What domain will this run on? (e.g., `dashboard.example.com`)
2. **Telegram Bot Token** — For Vel framework auth
3. **Telegram User IDs** — Who should have access?
4. **Port** — What port to run on? (default: `3700`)
5. **Install directory** — Where to install Vel? (default: `/opt/vel`)

---

## Step 1 — Install the Vel framework

VelBrowser is a Vel app. Install Vel first:

```bash
# Clone Vel
git clone https://github.com/essdee/vel.git <install-dir>
cd <install-dir>

# Install Go (if not present)
go version || {
  curl -LO https://go.dev/dl/go1.24.1.linux-amd64.tar.gz
  sudo tar -C /usr/local -xzf go1.24.1.linux-amd64.tar.gz
  export PATH=$PATH:/usr/local/go/bin
  echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
}

# Initial build (will rebuild after adding apps)
go build -o vel .

# Create config
cp config.example.json config.json
```

Edit `config.json` with the user's domain, bot token, and allowed users. See [Vel AGENT-SETUP.md](https://github.com/essdee/vel/blob/main/AGENT-SETUP.md) for full details.

```bash
echo "BOT_TOKEN=<token>" > <install-dir>/.env
```

---

## Step 2 — Install VelBrowser

```bash
cd <install-dir>/apps/
git clone https://github.com/karthikeyan5/velbrowser.git
```

Rebuild Vel to include VelBrowser's server-side code:

```bash
cd <install-dir>
go run . build --mode=bypass
```

This scans VelBrowser's Go server code, generates imports, and compiles a single binary with the relay endpoints included.

---

## Step 3 — Configure

VelBrowser works out of the box with no extra configuration. The relay endpoints are automatically registered at `/relay/*`.

To configure OpenClaw to use this relay, set the relay URL in your OpenClaw config:

```bash
# In your OpenClaw gateway config, set:
# relay_url: https://<domain>/relay
```

---

## Step 4 — Deploy

### systemd service

Create `/etc/systemd/system/vel.service`:

```ini
[Unit]
Description=Vel Dashboard + VelBrowser
After=network.target

[Service]
Type=simple
WorkingDirectory=<install-dir>
ExecStart=<install-dir>/vel
EnvironmentFile=<install-dir>/.env
Restart=always
RestartSec=5
User=<username>
Environment=HOME=/home/<username>  # REQUIRED: systemd doesn't always set HOME correctly
Environment=WORKSPACE=/home/<username>/.openclaw/workspace  # REQUIRED: panels read data files from this directory

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable vel
sudo systemctl start vel
```

### Expose to the internet

**⏸️ STOP — Ask the user how they want to expose the dashboard before proceeding.**

> How would you like to expose VelBrowser to the internet?
>
> 1. **Nginx + Let's Encrypt** — if you already have nginx installed
> 2. **Caddy** — automatic HTTPS, simpler config
> 3. **Cloudflare Tunnel** — no open ports needed, zero-trust
> 4. **Direct / I already have a reverse proxy** — just tell me the port
> 5. **I don't know, guide me** — I'll check your setup and recommend the simplest option

**Wait for the user's answer before proceeding.** If they pick option 5, check what's already installed (`which nginx`, `which caddy`, `which cloudflared`) and recommend **Caddy** for simplicity if nothing is installed.

---

#### Option A: Nginx + Let's Encrypt

```bash
sudo apt install -y nginx certbot python3-certbot-nginx
```

Create `/etc/nginx/sites-available/vel`:

```nginx
server {
    listen 80;
    server_name <domain>;

    location / {
        proxy_pass http://127.0.0.1:<port>;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

```bash
sudo ln -sf /etc/nginx/sites-available/vel /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d <domain>
```

#### Option B: Caddy

Add to `/etc/caddy/Caddyfile`:

```
<domain> {
    reverse_proxy localhost:<port>
}
```

```bash
sudo systemctl restart caddy
```

#### Option C: Cloudflare Tunnel

See [Cloudflare Tunnel docs](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/). Create a tunnel pointing to `http://localhost:<port>`.

#### Option D: Direct / Existing reverse proxy

Vel runs on `http://localhost:<port>`. Point your existing reverse proxy there.

> **⚠️ WebSocket support is required** for all reverse proxy options. Make sure your proxy forwards `Upgrade` and `Connection` headers — without this, the browser relay will fail.

### Verify

```bash
# Check relay is running
curl -s https://<domain>/relay/status

# Check CDP endpoint
curl -s https://<domain>/relay/cdp/json/version
```

---

## Step 5 — Telegram Bot Setup

### A) Set the Menu Button (automated)

Run this to add a "🌐 Browser Relay" button to the bot's chat menu:

```bash
curl -s -X POST "https://api.telegram.org/bot<BOT_TOKEN>/setChatMenuButton" \
  -H "Content-Type: application/json" \
  -d '{
    "menu_button": {
      "type": "web_app",
      "text": "🌐 Browser Relay",
      "web_app": {"url": "https://<domain>/dashboard"}
    }
  }'
```

### B) Set the Login Widget Domain (manual — no API)

**⏸️ Ask the user to do this step manually and confirm when done:**

> Open **@BotFather** → `/mybots` → select your bot → **Bot Settings** → **Domain** → enter: `<domain>`
>
> This is required for the Telegram Login Widget to work. There is no API for this — it must be done in BotFather.
>
> Let me know when you've done this.

---

## Step 6 — Connect a browser

1. Install the OpenClaw Browser Relay Chrome extension
2. Click the extension icon on a tab
3. The extension will connect to `wss://<domain>/relay/ws`
4. Use pairing codes or direct token auth to establish the session

---

## Troubleshooting

- **WebSocket connection fails** → Ensure nginx has `Upgrade` and `Connection` headers set
- **Pairing code expired** → Codes expire after 5 minutes, request a new one
- **No targets showing** → Browser extension must be connected and have tabs open
- **CDP proxy not working** → Check that the relay token matches between browser and agent
