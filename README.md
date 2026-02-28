<h1 align="center">VelReach</h1>

<p align="center">
  <strong>Your agent can use your browser.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/version-1.0.0-c9a84c?style=flat-square" alt="Version">
  <img src="https://img.shields.io/badge/Vel_app-⚡-ff6b35?style=flat-square" alt="Vel App">
  <img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="License">
</p>

---

Your AI agent is powerful — until it needs to log into a website, fill out a form, or check something behind a login. Then you're back to doing it yourself.

**VelReach lets your agent use your browser.** The one you're already logged into. With your cookies, your sessions, your saved passwords. No credentials shared. Ever.

Pair with a 6-character code. Your agent gets access. You watch everything it does in real time.

---

## How it works

1. **Run the launcher** on your machine. It opens Chrome and gives you a pairing code.
2. **Tell your agent the code.** It connects.
3. **That's it.** Your agent can now see and control your browser.

You stay logged in everywhere. Your agent uses those sessions directly. Need it to check your GST portal? Your bank dashboard? Your analytics? It just opens the tab and does it — because you're already authenticated.

When you're done, close the tab. Connection ends. You're always in control.

---

## Watch it work

This isn't a black box. VelReach runs in *your* browser on *your* machine. You see every page load, every click, every form fill as it happens. Your agent works, you watch.

If something looks wrong, close the tab. Done.

---

## Built for the real web

- **Human-like mouse movements** — Bezier curves, natural speed. No teleporting cursors that trigger bot detection.
- **No credentials needed** — Your browser is already logged in. Your agent uses that.
- **Full Chrome DevTools Protocol** — Your agent sees the page the way a developer does.
- **Works with any site** — If you can open it in Chrome, your agent can use it.

---

## Stop waiting for PRs

Need your agent to handle a new kind of website? A different workflow? You don't file an issue and wait. Your agent already has full browser access — it figures out the page and does the work. The [Vel](https://github.com/essdee/vel) framework underneath ensures the connection stays stable and secure while your agent does whatever it needs to do.

---

## Get started

```bash
cd your-vel-app/apps/
git clone https://github.com/karthikeyan5/velreach.git
cd /path/to/vel && ./vel build && ./vel start
```

Then run the launcher script on your machine to pair your browser.

📖 **[Launcher setup & architecture →](./RELAY.md)**

---

## Part of the Vel ecosystem

VelReach is a [Vel](https://github.com/essdee/vel) app. It pairs naturally with [Velboard](https://github.com/karthikeyan5/velboard) — the dashboard that builds itself. Together, your agent can see everything and do anything.

---

## License

[MIT](./LICENSE)
