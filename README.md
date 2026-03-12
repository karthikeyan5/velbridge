<p align="center">
  <strong>🌉 VelBridge</strong>
</p>

<h1 align="center">Your agent controls your browser.</h1>

<p align="center">
  Your AI agent runs on a VPS. Your browser runs on your laptop.<br>
  VelBridge connects them. Securely. In seconds.
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-1.0.0-c9a84c?style=flat-square" alt="Version">
  <img src="https://img.shields.io/badge/protocol-CDP-blue?style=flat-square" alt="CDP">
  <img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="License">
</p>

---

## The Problem

Your AI agent can do amazing things — but it can't click a button on a website. It can't log into your bank. It can't fill out a form on a site that blocks bots.

Headless browsers are detectable. Browser automation from the server is fragile. What if your agent could just use **your actual browser**?

## How It Works

```
Your Agent (VPS)  ←→  VelBridge Relay  ←→  Your Browser (Laptop)
```

1. **You** open the VelBridge launcher on your computer
2. **Enter** a 6-digit pairing code from your Telegram bot
3. **Done** — your agent can now control your browser via Chrome DevTools Protocol

No passwords shared. No browser data leaves your machine. The relay just forwards commands.

---

## What makes it different

| Feature | VelBridge | SweetLink | Playwright |
|---------|-----------|-----------|------------|
| Setup | Download + pair code | Node.js + pnpm + mkcert + TLS | npm install + code |
| Works with your tabs | ✅ Your actual browser | ✅ Controlled Chrome | ❌ Headless only |
| Zero config | ✅ | ❌ | ❌ |
| Human-like interaction | ✅ Bézier mouse curves | ❌ | ❌ |
| Cross-network | ✅ Agent on VPS, browser on laptop | ✅ | ❌ Local only |

---

## Features

- **🔑 Pairing code flow** — No API keys or tokens to manage. Just a 6-digit code.
- **🖱️ Human-like interaction** — Bézier curve mouse movement, realistic typing delays
- **🔒 Secure** — WebSocket relay, no browser data stored server-side
- **📡 Cross-network** — Agent anywhere, browser anywhere
- **🖥️ Dashboard panel** — See connection status from your Vel dashboard

---

## Install

```bash
cd /path/to/vel/apps/
git clone https://github.com/karthikeyan5/velbridge.git
cd /path/to/vel/ && ./vel build
```

Then download the launcher for your OS from the VelBridge page in your dashboard.

### macOS users

If Gatekeeper blocks the launcher, open Terminal and run:
```bash
curl -sSL https://your-dashboard/relay/launcher?platform=mac | bash
```

---

## Architecture

```
┌──────────────┐     WebSocket      ┌──────────────┐     CDP      ┌──────────────┐
│   AI Agent   │ ←──────────────── │  Vel Server   │ ←─────────── │  Your Chrome  │
│   (VPS)      │     commands       │  (Relay)      │   bridge     │  (Laptop)     │
└──────────────┘     + responses    └──────────────┘   page       └──────────────┘
```

The bridge page runs in your Chrome and connects to the browser's DevTools WebSocket. The relay server forwards CDP commands from your agent. No data is stored — it's a pure pass-through.

---

## Built on Vel

VelBridge is a [Vel](https://github.com/essdee/vel) app. It registers routes, a dashboard panel, and Go server code — all compiled into Vel's single binary.

---

<p align="center">
  <sub>Built on <a href="https://github.com/essdee/vel">⚡ Vel</a> — the framework where AI builds and the framework guarantees.</sub>
</p>
