package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	vel "vel/pkg/vel"
)

var (
	cachedBotUsername string
	botUsernameMu     sync.Mutex
)

// getBotUsername fetches the bot username from Telegram API (cached after first call).
func getBotUsername() string {
	botUsernameMu.Lock()
	defer botUsernameMu.Unlock()

	if cachedBotUsername != "" {
		return cachedBotUsername
	}

	token := vel.GetBotToken()
	if token == "" {
		return "bot"
	}

	resp, err := http.Get("https://api.telegram.org/bot" + token + "/getMe")
	if err != nil {
		return "bot"
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || !result.OK {
		return "bot"
	}

	cachedBotUsername = result.Result.Username
	return cachedBotUsername
}

func deriveBaseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		} else {
			scheme = "http"
		}
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func deriveWSURL(r *http.Request) string {
	scheme := "wss"
	if r.TLS == nil {
		if fwd := r.Header.Get("X-Forwarded-Proto"); fwd == "http" || fwd == "" {
			scheme = "ws"
		}
	}
	return fmt.Sprintf("%s://%s/relay/ws", scheme, r.Host)
}

// HandleBridge serves the bridge HTML page.
// Injects bot username. No auth required — pairing happens in-browser.
func (rl *Relay) HandleBridge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	botUser := getBotUsername()
	html := strings.ReplaceAll(bridgeHTML, "__BOT_USERNAME__", botUser)
	w.Write([]byte(html))
}

// HandleDownload generates platform-specific launcher scripts.
// No auth required — pairing happens at runtime.
func (rl *Relay) HandleDownload(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	if platform == "" {
		http.Error(w, "Invalid platform. Use: linux, mac, windows", 400)
		return
	}

	baseURL := deriveBaseURL(r)

	var script, filename string
	switch platform {
	case "linux":
		script = generateLinuxScript(baseURL)
		filename = "openclaw-browser.sh"
	case "mac":
		script = generateMacScript(baseURL)
		filename = "openclaw-browser.command"
	case "windows":
		script = generateWindowsScript(baseURL)
		filename = "openclaw-browser.bat"
	default:
		http.Error(w, "Invalid platform. Use: linux, mac, windows", 400)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Write([]byte(script))
}

// bridgeHTML is the bridge page served from the server over HTTPS.
// Handles pairing in-browser, polls for CDP WS URL from launcher.
const bridgeHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>OpenClaw Browser</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { background: #0e0e12; color: #e0e0e0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif; min-height: 100vh; display: flex; align-items: center; justify-content: center; }
  .container { max-width: 480px; width: 100%; padding: 40px 24px; text-align: center; }
  .logo { font-size: 48px; margin-bottom: 8px; }
  h1 { font-size: 22px; font-weight: 600; color: #fff; margin-bottom: 32px; }
  .status-icon { font-size: 40px; margin-bottom: 12px; }
  .status-text { font-size: 18px; font-weight: 500; }
  .status-sub { font-size: 14px; color: #888; margin-top: 8px; line-height: 1.6; }
  .card { background: #16161c; border: 1px solid #2a2a35; border-radius: 12px; padding: 24px; margin-bottom: 16px; }
  @keyframes glow { 0%,100% { box-shadow: 0 0 20px rgba(34,197,94,0.2); } 50% { box-shadow: 0 0 40px rgba(34,197,94,0.4); } }
  @keyframes aglow { 0%,100% { box-shadow: 0 0 20px rgba(0,170,255,0.2); } 50% { box-shadow: 0 0 40px rgba(0,170,255,0.5); } }
  @keyframes pulse { 0%,100% { opacity: 1; } 50% { opacity: 0.5; } }
  .card.connected { animation: glow 3s ease-in-out infinite; }
  .card.active { animation: aglow 1.5s ease-in-out infinite; }
  .card.pairing { border-color: #f59e0b33; }
  .tab-name { color: #0af; font-size: 14px; margin-top: 8px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 400px; margin: 8px auto 0; }
  .tips { display: flex; gap: 8px; flex-wrap: wrap; justify-content: center; margin-bottom: 24px; }
  .tip { background: #1a1a24; border: 1px solid #2a2a35; border-radius: 6px; padding: 6px 12px; font-size: 12px; color: #888; }
  .btn { background: transparent; border: 1px solid #444; color: #aaa; padding: 8px 20px; border-radius: 6px; cursor: pointer; font-size: 13px; }
  .btn:hover { border-color: #ef4444; color: #ef4444; }
  .btn-primary { border-color: #0af; color: #0af; margin-top: 12px; }
  .btn-primary:hover { background: rgba(0,170,255,0.1); border-color: #0af; color: #0af; }
  .hidden { display: none; }
  .code { font-size: 36px; font-weight: 700; letter-spacing: 8px; color: #f59e0b; font-family: 'SF Mono', 'Fira Code', monospace; margin: 16px 0 8px; }
  .code-hint { font-size: 13px; color: #888; margin-bottom: 4px; }
  .tg-link { display: inline-block; background: rgba(0,170,255,0.1); border: 1px solid rgba(0,170,255,0.2); border-radius: 8px; padding: 10px 20px; color: #0af; text-decoration: none; font-weight: 600; font-size: 14px; margin-top: 8px; transition: all 0.2s; }
  .tg-link:hover { background: rgba(0,170,255,0.2); }
  .waiting-dots { animation: pulse 1.5s infinite; }
</style>
</head>
<body>
<div class="container">
  <div class="logo">🦞</div>
  <h1>OpenClaw Browser</h1>
  <div class="card" id="statusCard">
    <div class="status-icon" id="statusIcon">⏳</div>
    <div class="status-text" id="statusText">Starting...</div>
    <div class="status-sub" id="statusSub"></div>
    <div class="tab-name hidden" id="tabName"></div>
    <div id="pairingUI" class="hidden"></div>
  </div>
  <div class="tips">
    <span class="tip">💡 Keep this tab open</span>
    <span class="tip">🖥️ You can use other apps</span>
  </div>
  <button class="btn" onclick="disconnect()">Disconnect</button>
</div>
<script>
(function() {
  var BOT = '__BOT_USERNAME__';
  var ORIGIN = window.location.origin;
  var params = new URLSearchParams(window.location.search);
  var launcherID = params.get('launcher') || '';

  var $ = function(id) { return document.getElementById(id); };
  var relayWS = null;
  var browserWS = null;
  var browserWSURL = null;
  var relayToken = localStorage.getItem('openclaw_relay_token') || '';
  var sessionMap = {};
  var targetMap = {};
  var targets = [];
  var cdpId = 1;
  var refreshInterval = null;

  function setStatus(icon, text, sub, cls) {
    $('statusIcon').textContent = icon;
    $('statusText').textContent = text;
    $('statusSub').innerHTML = sub || '';
    $('statusCard').className = 'card' + (cls ? ' ' + cls : '');
    $('tabName').classList.add('hidden');
    $('pairingUI').classList.add('hidden');
  }

  function showTab(name) {
    var el = $('tabName');
    el.textContent = '🔍 ' + name;
    el.classList.remove('hidden');
  }

  // ── Phase 1: Get relay token (pair or use saved) ──

  function start() {
    if (relayToken) {
      setStatus('🔑', 'Reconnecting...', 'Using saved session');
      fetch(ORIGIN + '/relay/cdp/status?token=' + relayToken)
        .then(function(r) { return r.ok ? r.json() : Promise.reject('bad'); })
        .then(function(d) {
          if (d.state !== undefined) { waitForWSURL(); }
          else { clearToken(); startPairing(); }
        })
        .catch(function() { clearToken(); startPairing(); });
      return;
    }
    startPairing();
  }

  function clearToken() {
    relayToken = '';
    localStorage.removeItem('openclaw_relay_token');
  }

  function startPairing() {
    setStatus('🔗', 'Pairing required', '', 'pairing');
    $('pairingUI').classList.remove('hidden');

    fetch(ORIGIN + '/relay/pair/new')
      .then(function(r) { return r.json(); })
      .then(function(d) {
        if (d.error) {
          $('pairingUI').innerHTML = '<div class="status-sub">❌ ' + d.error + '</div><button class="btn btn-primary" onclick="location.reload()">Try Again</button>';
          return;
        }
        var code = d.code;
        var pairingToken = d.token;

        $('pairingUI').innerHTML =
          '<div class="code">' + code + '</div>' +
          '<div class="code-hint">Send this code to Ram on Telegram</div>' +
          '<a class="tg-link" href="https://t.me/' + BOT + '?start=pair_' + code + '" target="_blank">💬 Open Telegram</a>' +
          '<div class="code-hint" style="margin-top:16px"><span class="waiting-dots">⏳</span> Waiting for pairing...</div>';

        pollPairing(pairingToken, 0);
      })
      .catch(function() {
        $('pairingUI').innerHTML = '<div class="status-sub">❌ Could not reach server</div><button class="btn btn-primary" onclick="location.reload()">Retry</button>';
      });
  }

  function pollPairing(pairingToken, attempt) {
    if (attempt >= 150) {
      setStatus('⏰', 'Pairing expired', '');
      $('pairingUI').classList.remove('hidden');
      $('pairingUI').innerHTML = '<button class="btn btn-primary" onclick="location.reload()">Try Again</button>';
      return;
    }
    setTimeout(function() {
      fetch(ORIGIN + '/relay/pair/status?token=' + pairingToken)
        .then(function(r) { return r.json(); })
        .then(function(d) {
          if (d.activated && d.relayToken) {
            relayToken = d.relayToken;
            localStorage.setItem('openclaw_relay_token', relayToken);
            setStatus('✅', 'Paired!', 'Connecting to browser...');
            waitForWSURL();
          } else {
            pollPairing(pairingToken, attempt + 1);
          }
        })
        .catch(function() { pollPairing(pairingToken, attempt + 1); });
    }, 2000);
  }

  // ── Phase 2: Get browser WS URL from launcher ──

  function waitForWSURL() {
    if (!launcherID) {
      setStatus('⏳', 'Waiting for browser...', 'No launcher ID — start the launcher script');
      return;
    }
    setStatus('⏳', 'Waiting for browser...', '<span class="waiting-dots">Connecting to Chrome CDP...</span>');
    pollWSURL(0);
  }

  function pollWSURL(attempt) {
    if (attempt >= 60) {
      setStatus('❌', 'Browser not found', 'Launcher did not report CDP info. Try restarting.');
      return;
    }
    setTimeout(function() {
      fetch(ORIGIN + '/relay/cdp-info?launcher=' + launcherID)
        .then(function(r) { return r.json(); })
        .then(function(d) {
          if (d.wsUrl) {
            browserWSURL = d.wsUrl;
            connectBrowser();
          } else {
            pollWSURL(attempt + 1);
          }
        })
        .catch(function() { pollWSURL(attempt + 1); });
    }, 1000);
  }

  // ── Phase 3: Connect to browser CDP ──

  function refreshTargets() {
    if (!browserWS || browserWS.readyState !== 1) return;
    browserWS.send(JSON.stringify({ id: cdpId++, method: 'Target.getTargets' }));
  }

  function attachTarget(targetId) {
    if (targetMap[targetId]) return;
    browserWS.send(JSON.stringify({
      id: cdpId++,
      method: 'Target.attachToTarget',
      params: { targetId: targetId, flatten: true }
    }));
  }

  function detachTarget(targetId) {
    var sid = targetMap[targetId];
    if (!sid) return;
    browserWS.send(JSON.stringify({
      id: cdpId++,
      method: 'Target.detachFromTarget',
      params: { sessionId: sid }
    }));
    delete targetMap[targetId];
    delete sessionMap[sid];
  }

  function filterTargets(list) {
    return list.filter(function(t) {
      return t.type === 'page' &&
        !t.url.startsWith('chrome://') &&
        !t.url.startsWith('chrome-extension://') &&
        !t.url.startsWith('devtools://') &&
        t.url !== '' &&
        t.url !== 'about:blank';
    });
  }

  function connectBrowser() {
    setStatus('🔗', 'Connecting to browser...', '');
    browserWS = new WebSocket(browserWSURL);

    browserWS.onopen = function() {
      console.log('[bridge] connected to browser CDP');
      browserWS.send(JSON.stringify({ id: cdpId++, method: 'Target.setDiscoverTargets', params: { discover: true } }));
      refreshTargets();
      refreshInterval = setInterval(refreshTargets, 5000);
      connectRelay();
    };

    browserWS.onmessage = function(e) {
      var msg = JSON.parse(e.data);

      if (msg.result && msg.result.targetInfos) {
        targets = filterTargets(msg.result.targetInfos);
        if (relayWS && relayWS.readyState === 1) {
          var relayTargets = targets.map(function(t) {
            return {
              id: t.targetId, title: t.title, url: t.url, type: t.type,
              webSocketDebuggerUrl: 'ws://localhost:9222/devtools/page/' + t.targetId
            };
          });
          relayWS.send(JSON.stringify({ type: 'targets', data: relayTargets }));
        }
        return;
      }

      if (msg.method === 'Target.attachedToTarget') {
        var sessionId = msg.params.sessionId;
        var targetId = msg.params.targetInfo.targetId;
        sessionMap[sessionId] = targetId;
        targetMap[targetId] = sessionId;
        console.log('[bridge] attached', targetId, 'session', sessionId);
        return;
      }

      if (msg.method === 'Target.detachedFromTarget') {
        var sid = msg.params.sessionId;
        var tid = sessionMap[sid];
        if (tid) delete targetMap[tid];
        delete sessionMap[sid];
        return;
      }

      if (msg.method === 'Target.targetCreated' || msg.method === 'Target.targetInfoChanged' || msg.method === 'Target.targetDestroyed') {
        refreshTargets();
        return;
      }

      if (msg.sessionId) {
        // Target-specific response
        var tid2 = sessionMap[msg.sessionId];
        if (tid2 && relayWS && relayWS.readyState === 1) {
          relayWS.send(JSON.stringify({ type: 'cdp', targetId: tid2, data: msg }));
        }
        return;
      }

      // Browser-level response (no sessionId) — forward as-is
      if (msg.id && relayWS && relayWS.readyState === 1) {
        relayWS.send(JSON.stringify({ type: 'cdp', data: msg }));
        return;
      }
    };

    browserWS.onclose = function() {
      console.log('[bridge] browser WS closed');
      if (refreshInterval) clearInterval(refreshInterval);
      sessionMap = {};
      targetMap = {};
      setStatus('❌', 'Browser disconnected', 'Reconnecting in 3s...');
      setTimeout(connectBrowser, 3000);
    };

    browserWS.onerror = function() {};
  }

  // ── Phase 4: Connect to relay server ──

  var relayFailCount = 0;

  function connectRelay() {
    var wsScheme = ORIGIN.startsWith('https') ? 'wss' : 'ws';
    var wsHost = ORIGIN.replace(/^https?:\/\//, '');
    var relayURL = wsScheme + '://' + wsHost + '/relay/ws';

    setStatus('🔗', 'Connecting to relay...', '');
    relayWS = new WebSocket(relayURL + '?token=' + relayToken);

    relayWS.onopen = function() {
      relayFailCount = 0;
      setStatus('✅', 'Connected!', 'Waiting for your AI...', 'connected');
      refreshTargets();
    };

    relayWS.onmessage = function(e) {
      var env = JSON.parse(e.data);
      switch (env.type) {
        case 'cdp': {
          if (browserWS && browserWS.readyState === 1) {
            var cdpMsg = env.data;
            if (env.targetId) {
              // Target-specific: inject sessionId
              var sid = targetMap[env.targetId];
              if (sid) cdpMsg.sessionId = sid;
            }
            // Browser-level commands (no targetId) forwarded directly
            browserWS.send(JSON.stringify(cdpMsg));
          }
          break;
        }
        case 'connect': {
          setStatus('🤖', 'AI is working...', '', 'active');
          var t = targets.find(function(x) { return x.targetId === env.targetId; });
          if (t) showTab(t.title);
          attachTarget(env.targetId);
          break;
        }
        case 'disconnect': {
          detachTarget(env.targetId);
          break;
        }
        case 'agent_disconnected': {
          setStatus('✅', 'Connected!', 'AI finished', 'connected');
          break;
        }
        case 'ping': {
          relayWS.send(JSON.stringify({ type: 'pong' }));
          break;
        }
      }
    };

    relayWS.onclose = function(e) {
      relayFailCount++;
      if (e.code === 4001 || e.code === 1008 || relayFailCount >= 3) {
        clearToken();
        relayFailCount = 0;
        setStatus('🔑', 'Session expired', '');
        startPairing();
        return;
      }
      setStatus('🔌', 'Relay disconnected', 'Reconnecting...');
      setTimeout(connectRelay, 3000);
    };

    relayWS.onerror = function() {};
  }

  window.disconnect = function() {
    if (relayWS) { relayWS.close(); relayWS = null; }
    if (browserWS) { browserWS.close(); browserWS = null; }
    if (refreshInterval) clearInterval(refreshInterval);
    sessionMap = {};
    targetMap = {};
    setStatus('🔌', 'Disconnected', 'You can close this tab');
  };

  start();
})();
</script>
</body>
</html>`

func generateLinuxScript(baseURL string) string {
	return fmt.Sprintf(`#!/bin/bash
# OpenClaw Browser Launcher

SERVER="%s"

CHROME=$(command -v google-chrome || command -v chromium-browser || command -v chromium 2>/dev/null)
if [ -z "$CHROME" ]; then
    echo "❌ Chrome/Chromium not found."
    exit 1
fi

# Generate launcher ID for bridge <-> launcher coordination
LAUNCHER_ID=$(head -c 8 /dev/urandom | xxd -p)

# Launch Chrome with CDP and bridge page from server
echo "🌐 Launching browser..."
"$CHROME" \
    --remote-debugging-port=9222 \
    "--remote-allow-origins=$SERVER" \
    --user-data-dir="$HOME/OpenClawBrowser" \
    --no-first-run \
    "$SERVER/relay/bridge?launcher=$LAUNCHER_ID" 2>/dev/null &
BROWSER_PID=$!

# Wait for CDP to be ready and get browser WS URL
echo -n "⏳ Waiting for CDP"
BROWSER_WS=""
for i in $(seq 1 30); do
    sleep 1
    VJSON=$(curl -s http://127.0.0.1:9222/json/version 2>/dev/null)
    if [ -n "$VJSON" ]; then
        BROWSER_WS=$(echo "$VJSON" | grep -o '"webSocketDebuggerUrl": *"[^"]*"' | head -1 | cut -d'"' -f4)
        if [ -n "$BROWSER_WS" ]; then break; fi
    fi
    echo -n "."
done
echo ""

if [ -z "$BROWSER_WS" ]; then
    echo "❌ Chrome CDP not responding on port 9222"
    kill $BROWSER_PID 2>/dev/null
    exit 1
fi

# Send WS URL to server so bridge page can pick it up
curl -s -X POST "$SERVER/relay/cdp-info?launcher=$LAUNCHER_ID" \
    -H "Content-Type: application/json" \
    -d "{\"wsUrl\":\"$BROWSER_WS\"}" > /dev/null

echo "✅ Browser ready! Complete pairing in the browser tab."
echo "   Keep this terminal open. Close browser to stop."
wait $BROWSER_PID 2>/dev/null
echo "👋 Goodbye!"
`, baseURL)
}

func generateMacScript(baseURL string) string {
	return fmt.Sprintf(`#!/bin/bash
# OpenClaw Browser Launcher

SERVER="%s"

CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
if [ ! -f "$CHROME" ]; then
    CHROME=$(command -v chromium || command -v google-chrome || true)
fi
if [ -z "$CHROME" ] || [ ! -f "$CHROME" ]; then
    echo "❌ Chrome not found."
    exit 1
fi

LAUNCHER_ID=$(head -c 8 /dev/urandom | xxd -p)

echo "🌐 Launching browser..."
"$CHROME" \
    --remote-debugging-port=9222 \
    "--remote-allow-origins=$SERVER" \
    --user-data-dir="$HOME/OpenClawBrowser" \
    --no-first-run \
    "$SERVER/relay/bridge?launcher=$LAUNCHER_ID" 2>/dev/null &
BROWSER_PID=$!

echo -n "⏳ Waiting for CDP"
BROWSER_WS=""
for i in $(seq 1 30); do
    sleep 1
    VJSON=$(curl -s http://127.0.0.1:9222/json/version 2>/dev/null)
    if [ -n "$VJSON" ]; then
        BROWSER_WS=$(echo "$VJSON" | grep -o '"webSocketDebuggerUrl": *"[^"]*"' | head -1 | cut -d'"' -f4)
        if [ -n "$BROWSER_WS" ]; then break; fi
    fi
    echo -n "."
done
echo ""
if [ -z "$BROWSER_WS" ]; then echo "❌ CDP not ready"; kill $BROWSER_PID 2>/dev/null; exit 1; fi

curl -s -X POST "$SERVER/relay/cdp-info?launcher=$LAUNCHER_ID" \
    -H "Content-Type: application/json" \
    -d "{\"wsUrl\":\"$BROWSER_WS\"}" > /dev/null

echo "✅ Browser ready! Complete pairing in the browser tab."
echo "   Keep this terminal open."
wait $BROWSER_PID 2>/dev/null
echo "👋 Goodbye!"
`, baseURL)
}

func generateWindowsScript(baseURL string) string {
	return fmt.Sprintf(`@echo off
setlocal EnableDelayedExpansion
REM OpenClaw Browser Launcher

set "SERVER=%s"

echo Launching browser...
set "CHROME="
if exist "C:\Program Files\Google\Chrome\Application\chrome.exe" set "CHROME=C:\Program Files\Google\Chrome\Application\chrome.exe"
if exist "C:\Program Files (x86)\Google\Chrome\Application\chrome.exe" set "CHROME=C:\Program Files (x86)\Google\Chrome\Application\chrome.exe"
if "%%CHROME%%"=="" ( echo Chrome not found! & pause & exit /b 1 )

for /f "usebackq delims=" %%%%i in (`+"`"+`powershell -Command "[guid]::NewGuid().ToString('N').Substring(0,16)"`+"`"+`) do set "LAUNCHER_ID=%%%%i"

start "" "%%CHROME%%" --remote-debugging-port=9222 "--remote-allow-origins=%%SERVER%%" --user-data-dir="%%USERPROFILE%%\OpenClawBrowser" --no-first-run "%%SERVER%%/relay/bridge?launcher=%%LAUNCHER_ID%%"

echo Waiting for CDP...
:CDPWAIT
powershell -Command "Start-Sleep 1"
for /f "usebackq delims=" %%%%i in (`+"`"+`powershell -Command "try { (Invoke-RestMethod 'http://localhost:9222/json/version').webSocketDebuggerUrl } catch { 'waiting' }"`+"`"+`) do set "BROWSER_WS=%%%%i"
if "%%BROWSER_WS%%"=="waiting" goto CDPWAIT

powershell -Command "Invoke-RestMethod -Method Post -Uri '%%SERVER%%/relay/cdp-info?launcher=%%LAUNCHER_ID%%' -ContentType 'application/json' -Body ('{\"wsUrl\":\"' + '%%BROWSER_WS%%' + '\"}')" >nul 2>&1

echo Ready! Complete pairing in the browser tab.
echo Keep this window open.
pause
`, baseURL)
}
