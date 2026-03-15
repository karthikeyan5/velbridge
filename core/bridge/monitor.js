/**
 * VelBridge Proxy Mode — Monitor Script
 * Injected into proxied pages. Captures console, errors, network, and OAuth.
 * Connects via WebSocket to the Vel server.
 */
(function() {
  'use strict';
  if (window.__velMonitorActive) return;
  window.__velMonitorActive = true;

  var sessionId = window.__velSession || '';
  var domain = window.__velDomain || '';
  var ws = null;
  var wsReady = false;
  var msgQueue = [];

  function send(obj) {
    var data = JSON.stringify(obj);
    if (ws && wsReady) {
      ws.send(data);
    } else {
      msgQueue.push(data);
    }
  }

  function flushQueue() {
    while (msgQueue.length > 0 && ws && wsReady) {
      ws.send(msgQueue.shift());
    }
  }

  function connectWS() {
    var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    var url = proto + '//' + location.host + '/bridge/proxy/_ws?session=' + sessionId;
    ws = new WebSocket(url);

    ws.onopen = function() {
      wsReady = true;
      flushQueue();
      send({ type: 'connected', url: location.href, domain: domain, ts: Date.now() });
    };

    ws.onclose = function() {
      wsReady = false;
      // Reconnect after 3s
      setTimeout(connectWS, 3000);
    };

    ws.onerror = function() {};

    ws.onmessage = function(event) {
      // Command handling is in commands.js
      if (window.__velHandleCommand) {
        window.__velHandleCommand(JSON.parse(event.data));
      }
    };
  }

  // ── Console Capture ──
  ['log', 'error', 'warn', 'info', 'debug'].forEach(function(method) {
    var orig = console[method].bind(console);
    console[method] = function() {
      var args = Array.prototype.slice.call(arguments);
      try {
        send({
          type: 'console', method: method,
          args: args.map(function(a) {
            try { return typeof a === 'object' ? JSON.stringify(a) : String(a); }
            catch(e) { return String(a); }
          }),
          ts: Date.now()
        });
      } catch(e) {}
      orig.apply(console, args);
    };
  });

  // ── Error Capture ──
  window.addEventListener('error', function(e) {
    send({
      type: 'error', message: e.message, source: e.filename,
      line: e.lineno, col: e.colno,
      stack: e.error ? e.error.stack : undefined,
      ts: Date.now()
    });
  });

  window.addEventListener('unhandledrejection', function(e) {
    send({
      type: 'error',
      message: 'Unhandled Promise Rejection: ' + String(e.reason),
      stack: e.reason ? e.reason.stack : undefined,
      ts: Date.now()
    });
  });

  // ── Network Capture (fetch) ──
  var origFetch = window.fetch;
  window.fetch = function() {
    var args = arguments;
    var url = typeof args[0] === 'string' ? args[0] : (args[0] && args[0].url) || '';
    var method = (args[1] && args[1].method) || 'GET';
    var id = Math.random().toString(36).slice(2, 8);
    var start = performance.now();

    send({ type: 'net', id: id, method: method, url: url, status: 'pending', ts: Date.now() });

    return origFetch.apply(this, args).then(function(res) {
      send({
        type: 'net', id: id, method: method, url: url, status: res.status,
        ms: Math.round(performance.now() - start), ts: Date.now()
      });
      return res;
    }).catch(function(err) {
      send({
        type: 'net', id: id, method: method, url: url, status: 'error',
        error: err.message, ts: Date.now()
      });
      throw err;
    });
  };

  // ── Network Capture (XHR) ──
  var origOpen = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = function(method, url) {
    this._vel = { method: method, url: url, id: Math.random().toString(36).slice(2, 8), start: performance.now() };
    return origOpen.apply(this, arguments);
  };
  var origSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.send = function() {
    if (this._vel) {
      var v = this._vel;
      send({ type: 'net', id: v.id, method: v.method, url: v.url, status: 'pending', ts: Date.now() });
      this.addEventListener('load', function() {
        send({
          type: 'net', id: v.id, method: v.method, url: v.url,
          status: this.status, ms: Math.round(performance.now() - v.start), ts: Date.now()
        });
      });
    }
    return origSend.apply(this, arguments);
  };

  // ── Service Worker Block ──
  if (navigator.serviceWorker) {
    navigator.serviceWorker.register = function() { return Promise.resolve(null); };
  }

  // ── WebSocket Interception ──
  var OrigWS = window.WebSocket;
  window.WebSocket = function(url, protocols) {
    // Don't intercept our own connections
    if (url && url.indexOf('/bridge/proxy/_ws') !== -1) {
      return new OrigWS(url, protocols);
    }
    // Route through proxy relay
    var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    var proxiedUrl = proto + '//' + location.host + '/bridge/proxy/_wsproxy?target=' + encodeURIComponent(url);
    return new OrigWS(proxiedUrl, protocols);
  };
  // Copy static properties
  Object.keys(OrigWS).forEach(function(k) { window.WebSocket[k] = OrigWS[k]; });
  window.WebSocket.prototype = OrigWS.prototype;

  // ── OAuth Detection ──
  var oauthDomains = [
    'accounts.google.com', 'github.com/login/oauth',
    'facebook.com/v', 'login.microsoftonline.com',
    'appleid.apple.com/auth', 'auth0.com'
  ];

  document.addEventListener('click', function(e) {
    var link = e.target.closest ? e.target.closest('a') : null;
    if (!link || !link.href) return;
    var isOAuth = oauthDomains.some(function(d) { return link.href.indexOf(d) !== -1; });
    if (isOAuth) {
      e.preventDefault();
      showOAuthNotice(link.href);
    }
  }, true);

  function showOAuthNotice(originalUrl) {
    var overlay = document.createElement('div');
    overlay.id = '__vel_oauth_overlay';
    overlay.innerHTML =
      '<div style="position:fixed;top:0;left:0;right:0;bottom:0;background:rgba(0,0,0,0.7);z-index:99999;' +
      'display:flex;align-items:center;justify-content:center;font-family:system-ui,sans-serif">' +
      '<div style="background:white;padding:32px;border-radius:12px;max-width:480px;color:#333">' +
      '<h2 style="margin:0 0 12px">🔒 External Login Detected</h2>' +
      '<p>This site uses external authentication which isn\'t supported in Proxy Mode.</p>' +
      '<p><strong>To import your login:</strong></p>' +
      '<ol><li>Click below to open the real site</li><li>Log in normally</li>' +
      '<li>Open DevTools → Application → Cookies</li><li>Copy session cookies and paste below</li></ol>' +
      '<a href="' + originalUrl + '" target="_blank" ' +
      'style="display:inline-block;padding:8px 16px;background:#2563eb;color:white;border-radius:6px;text-decoration:none;margin:8px 0">Open Real Site →</a>' +
      '<div style="margin-top:16px">' +
      '<textarea id="__vel_cookie_input" placeholder="Paste cookies here (name=value, one per line)" ' +
      'style="width:100%;height:80px;padding:8px;border:1px solid #ccc;border-radius:4px"></textarea>' +
      '<button onclick="window.__velImportCookies()" ' +
      'style="margin-top:8px;padding:8px 16px;background:#16a34a;color:white;border:none;border-radius:4px;cursor:pointer">Import Cookies</button>' +
      '</div>' +
      '<button onclick="document.getElementById(\'__vel_oauth_overlay\').remove()" ' +
      'style="margin-top:12px;padding:8px 16px;background:#eee;border:none;border-radius:4px;cursor:pointer;color:#333">Cancel</button>' +
      '</div></div>';
    document.body.appendChild(overlay);
  }

  window.__velImportCookies = function() {
    var textarea = document.getElementById('__vel_cookie_input');
    if (textarea && textarea.value) {
      send({ type: 'import_cookies', cookies: textarea.value, domain: domain });
      var overlay = document.getElementById('__vel_oauth_overlay');
      if (overlay) overlay.remove();
    }
  };

  // ── Keep-alive ping (detects silent WS death on mobile) ──
  var pingInterval = null;
  function startPing() {
    if (pingInterval) clearInterval(pingInterval);
    pingInterval = setInterval(function() {
      if (ws && wsReady) {
        try { ws.send(JSON.stringify({ type: 'ping', ts: Date.now() })); }
        catch(e) { wsReady = false; connectWS(); }
      }
    }, 15000); // every 15s
  }

  // ── Reconnect on tab visibility change (mobile app switching) ──
  document.addEventListener('visibilitychange', function() {
    if (document.visibilityState === 'visible') {
      // Tab became visible again — check if WS is still alive
      if (!ws || ws.readyState !== WebSocket.OPEN) {
        connectWS();
      } else {
        // Send a ping to verify connection is actually alive
        try { ws.send(JSON.stringify({ type: 'ping', ts: Date.now() })); }
        catch(e) { connectWS(); }
      }
    }
  });

  // ── Expose send function and connect ──
  window.__velSend = send;
  window.__velWS = function() { return ws; };

  window.__velInit = function() {
    sessionId = window.__velSession || sessionId;
    domain = window.__velDomain || domain;
    connectWS();
    startPing();
  };

  // Auto-init if session is already set
  if (sessionId) { connectWS(); startPing(); }
})();
