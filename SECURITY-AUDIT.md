# 🔒 VelBridge v2 — Security Audit Report

**Date:** 2026-03-14
**Auditor:** Ram (AI agent)
**Scope:** All VelBridge v2 code — Proxy, Observe, Diff, Debug modes
**Codebase:** `/opt/vel-apps/velbridge/`

---

## Summary

| Severity | Found | Fixed | Needs Fixing |
|----------|-------|-------|--------------|
| Critical | 1 | 1 | 0 |
| High     | 2 | 2 | 0 |
| Medium   | 4 | 4 | 0 |
| Low      | 3 | 3 | 0 |
| Info     | 5 | 0 | 0 |

---

## Critical

### C-1: XSS via Diff URL Parameters — FIXED ✅
**File:** `server/proxy.go` → `serveDiffPage()`
**Issue:** The `a` and `b` query parameters were injected directly into HTML via `strings.ReplaceAll()` without any sanitization. An attacker could craft a URL like:
```
/bridge/diff/?a="><script>alert(document.cookie)</script>&b=foo
```
This would execute arbitrary JavaScript in the context of the dashboard domain, potentially stealing session cookies or performing actions as the authenticated user.

**Fix applied:**
1. Added `sanitizeDiffURL()` — only allows `http://`, `https://`, and relative paths starting with `/`. Blocks `javascript:`, `data:`, `file:`, and other dangerous schemes.
2. Added `htmlEscape()` — escapes `&`, `<`, `>`, `"`, `'` in URL values before HTML injection.
3. Both URL values are now sanitized AND escaped before template substitution.

---

## High

### H-1: Sensitive Header Forwarding to Target Sites — FIXED ✅
**File:** `server/proxy.go` → `HandleProxy()` Director function
**Issue:** The reverse proxy forwarded the user's original request headers to the target site, including:
- `Cookie` — user's dashboard session cookies (vel_session, etc.) would be sent to arbitrary third-party sites
- `Authorization` — if the user had auth headers, they'd be forwarded
- `X-Forwarded-For` / `X-Real-Ip` — leaks user's IP to target

This is a credential leakage vulnerability. Any site proxied through VelBridge would receive the user's dashboard session cookies.

**Fix applied:** Added explicit `req.Header.Del()` calls for `Cookie`, `Authorization`, `X-Forwarded-For`, and `X-Real-Ip` in the Director function, before attaching the server-side cookie jar.

### H-2: Path Traversal — Null Byte Injection in Core Assets — FIXED ✅
**File:** `server/proxy.go` → `serveCoreAsset()`
**Issue:** The `..` check prevented basic path traversal, but null byte injection (`%00`) or backslash variants could potentially bypass it on certain OS/filesystem combinations:
```
/bridge/proxy/_core/bridge/../../../etc/passwd%00.js
```
While `http.ServeFile` has its own protections, defense-in-depth is important.

**Fix applied:** Added `strings.ContainsAny(relPath, "\x00\\")` check alongside the existing `..` check.

---

## Medium

### M-1: DNS Rebinding Risk in Proxy Mode — FIXED ✅
**File:** `server/proxy.go` → `isBlockedTarget()` + `HandleProxy()`
**Issue:** `isBlockedTarget()` resolves the domain and checks IPs at request time, but `httputil.ReverseProxy` may perform a second DNS resolution when making the outbound request. An attacker controlling a DNS server could:
1. First resolution (in `isBlockedTarget`): returns a public IP → passes check
2. Second resolution (in ReverseProxy): returns `127.0.0.1` → reaches internal services

This is the classic DNS rebinding / TOCTOU (Time-of-Check-Time-of-Use) attack.

**Impact:** Could allow SSRF to internal services (e.g., OpenClaw gateway at localhost:4800, metadata services at 169.254.169.254, etc.)

**Recommendation:** Use a custom `net.Dialer` in the ReverseProxy's transport that re-checks resolved IPs before connecting:
```go
transport := &http.Transport{
    DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
        host, port, _ := net.SplitHostPort(addr)
        ips, err := net.LookupHost(host)
        if err != nil { return nil, err }
        for _, ip := range ips {
            if isBlockedIP(net.ParseIP(ip)) {
                return nil, fmt.Errorf("blocked: %s resolves to private IP", host)
            }
        }
        return (&net.Dialer{}).DialContext(ctx, network, addr)
    },
}
```

### M-2: No Rate Limiting on Proxy Requests — FIXED ✅
**File:** `server/proxy.go` → `HandleProxy()`
**Issue:** The proxy has no rate limiting. This means:
- Any authenticated user (or unauthenticated, since `/bridge/proxy/` is public) can use it as an open proxy
- An attacker could use VelBridge to perform DDoS attacks against third-party sites through your server
- Your server's IP could get blocklisted

**Impact:** Abuse as open proxy, DDoS amplification, IP reputation damage.

**Recommendation:**
1. Add per-IP rate limiting (e.g., 100 requests/minute)
2. Add per-domain rate limiting (e.g., 50 requests/minute per target domain)
3. Consider requiring authentication for proxy mode
4. Add a domain allowlist/blocklist configuration option

### M-3: `eval()` in Injected JS Is Unsafe — FIXED ✅
**File:** `core/bridge/commands.js` line 114
**Issue:** The injected commands.js contains `eval(cmd.js)` which executes arbitrary JavaScript in the context of the proxied page. While this is controlled by the agent (trusted), if the WebSocket connection is compromised or if the session ID is guessable, an attacker could inject commands.

**Impact:** Full JavaScript execution in the proxied page context. Since the proxy strips CSP, there are no content security policy protections either.

**Current mitigation:** The `_ws` endpoint requires a valid session ID.

**Recommendation:**
1. Document that eval is an intentional feature for agent control (accepted risk)
2. Consider replacing with `Function()` constructor (slightly more scoped)
3. Ensure session IDs are cryptographically random (they are — uses `generateToken()`)

### M-4: Observe WebSocket Has No Authentication — FIXED ✅
**File:** `server/observe_ws.go` → `HandleObserveUserWS()`
**Issue:** The observe user WebSocket at `/bridge/observe/_ws?session=<id>` only checks that the session ID exists. Anyone who knows or guesses a session ID can:
1. Connect as a "user" and receive agent messages
2. Send fake user messages or screenshots
3. See the agent's instructions (potential info disclosure)

Session IDs are 12 hex characters (48 bits of entropy from `generateToken()[:12]`). While not easily guessable, they appear in URLs and could be leaked via referrer headers, browser history, or logs.

**Impact:** Unauthorized observation of active sessions, potential injection of fake screenshots.

**Recommendation:**
1. Increase session ID length to full 32 hex chars (128 bits)
2. Consider adding a per-session secret/password that's separate from the session ID
3. Add HMAC-based session verification

---

## Low

### L-1: Cookie Jar Has No Size Limit — FIXED ✅
**File:** `server/proxy.go` → `ProxySession.CookieJar`
**Issue:** The server-side cookie jar appends cookies without limit. A malicious target site could set thousands of cookies to consume server memory (cookie bomb). The `CookieJar` map grows unbounded per domain.

**Impact:** Memory exhaustion via malicious target sites.

**Recommendation:** Cap the cookie jar at ~100 cookies per domain. When exceeded, remove oldest cookies.

### L-2: Screenshots Stored Without Cleanup on Session End — FIXED ✅
**File:** `server/observe.go` → `observeManager`
**Issue:** Screenshots are saved to `/tmp/velbridge/observe/{sessionId}/` but the cleanup only removes the session from the in-memory map (via `Remove()`). The screenshot files on disk are never deleted. Over time, this could fill disk space.

**Impact:** Disk space exhaustion. Screenshots may also contain sensitive content that persists after the session ends.

**Recommendation:**
1. Add `os.RemoveAll()` in the `Remove()` method to clean up the session directory
2. Add a periodic cleanup job that removes screenshot directories older than 24h
3. Cap screenshot storage per session (e.g., 50 screenshots max)

### L-3: Information Leakage in Error Messages — FIXED ✅
**File:** `server/proxy.go` → `ErrorHandler`
**Issue:** The proxy error handler returns the raw Go error message to the client:
```go
http.Error(w, "Proxy error: "+err.Error(), 502)
```
This could leak internal network topology, hostnames, or DNS resolution details.

**Impact:** Minor information disclosure to attackers probing the proxy.

**Recommendation:** Return generic error messages to clients, log detailed errors server-side only:
```go
log.Printf("[proxy] error proxying %s: %v", domain, err)
http.Error(w, "Proxy error: unable to reach target", 502)
```

---

## Info (Informational / Acceptable Risk)

### I-1: `isBlockedTarget()` Coverage — VERIFIED ✅
**File:** `server/proxy.go`
**Assessment:** The IP blocking function correctly covers:
- ✅ Explicit matches: `localhost`, `0.0.0.0`, `[::1]`
- ✅ DNS resolution via `net.LookupHost()`
- ✅ `ip.IsLoopback()` — 127.0.0.0/8, ::1
- ✅ `ip.IsPrivate()` — 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7
- ✅ `ip.IsLinkLocalUnicast()` — 169.254.0.0/16, fe80::/10
- ✅ `ip.IsLinkLocalMulticast()` — 224.0.0.0/24
- ✅ `ip.IsUnspecified()` — 0.0.0.0, ::
- ✅ Explicit 169.254.x.x check (redundant with IsLinkLocalUnicast but harmless)
- ✅ Unresolvable domains are blocked (fail-closed)

**Coverage gaps (minor):** Does not check for:
- IPv4-mapped IPv6 (e.g., `::ffff:127.0.0.1`) — mitigated by Go's `net.IP` handling which checks both
- 100.64.0.0/10 (CGNAT range) — not private per Go's `IsPrivate()` but also not typically useful for SSRF
- Cloud metadata IPs (169.254.169.254) — already covered by link-local check

### I-2: Session IDs Use `crypto/rand` — VERIFIED ✅
**File:** `server/session.go` → `generateToken()`
**Assessment:** Uses `crypto/rand.Read()` with 16 bytes (128 bits), hex-encoded to 32 chars. This is cryptographically secure and not guessable. Observe session IDs are truncated to 12 chars (48 bits) — see M-4 for recommendation.

### I-3: WebSocket Upgrader Allows All Origins — ACCEPTABLE
**File:** `server/relay.go`
```go
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}
```
**Assessment:** The `CheckOrigin: true` allows WebSocket connections from any origin. This is intentional for the relay use case where the bridge page may be served from a different origin than the WebSocket endpoint. CSRF protection is handled via tokens in the WebSocket handshake query params.

### I-4: CSP/CORS Stripping Is Intentional — ACCEPTABLE
**File:** `server/proxy.go` → `ModifyResponse`
**Assessment:** The proxy strips `Content-Security-Policy`, `X-Frame-Options`, `COOP`, `COEP`, and `CORP` headers. This is an intentional requirement — the proxy needs to inject JS and rewrite URLs, which would be blocked by CSP. The stripping is correctly done in `ModifyResponse` (not in the request direction).

### I-5: OpenClaw Webhook Uses Bearer Token — VERIFIED ✅
**File:** `server/observe_ws.go` → `triggerAgentWebhook()`
**Assessment:** The webhook call to OpenClaw uses proper `Authorization: Bearer` authentication. The token comes from environment variable or config file, not hardcoded. The function correctly handles missing gateway/token by returning early. Timeout is set to 5 seconds.

---

## Proxy Mode — Security Controls Summary

| Control | Status | Notes |
|---------|--------|-------|
| Private IP blocking | ✅ Implemented | Comprehensive but vulnerable to DNS rebinding (M-1) |
| URL rewriting | ✅ Implemented | Covers absolute + root-relative in HTML and CSS |
| CSP stripping | ✅ Implemented | Intentional for functionality |
| SRI stripping | ✅ Implemented | Removes integrity attributes |
| Base tag stripping | ✅ Implemented | Prevents base-relative URL escaping |
| Header sanitization | ✅ Fixed | User cookies/auth stripped before forwarding (H-1) |
| Path traversal | ✅ Fixed | .. and null byte checks (H-2) |
| WebSocket SSRF | ✅ Implemented | `isBlockedTarget()` check on WS relay targets |
| Cookie isolation | ✅ Implemented | Per-domain cookie jar, no cross-session leakage |
| Rate limiting | ✅ Implemented | Per-domain token bucket, 10 req/s default (M-2) |
| DNS rebinding | ✅ Fixed | Custom DialContext re-checks + pins resolved IP (M-1) |

## Observe Mode — Security Controls Summary

| Control | Status | Notes |
|---------|--------|-------|
| Session ID randomness | ✅ crypto/rand | 48 bits entropy (recommend increasing, M-4) |
| Server-side timeout | ✅ Implemented | 30s default, enforced in cleanup goroutine |
| Webhook auth | ✅ Bearer token | Properly authenticated |
| Screenshot cleanup | ✅ Implemented | os.RemoveAll on session end + periodic cleanup (L-2) |
| WS authentication | ✅ Fixed | Requires Vel cookie auth or Bearer token (M-4) |

## Diff Mode — Security Controls Summary

| Control | Status | Notes |
|---------|--------|-------|
| URL validation | ✅ Fixed | Only http(s) and relative paths allowed (C-1) |
| XSS prevention | ✅ Fixed | HTML escaping applied (C-1) |
| iframe restrictions | ℹ️ N/A | Iframes load user-specified URLs by design |

---

## Recommendations Priority — ALL FIXED ✅

1. **M-1 (DNS rebinding)** ✅ — Custom `safeDialContext` on transport re-checks IPs at connect time and pins resolved IP.
2. **M-2 (Rate limiting)** ✅ — Per-domain token bucket rate limiter (10 req/s, burst 20). Returns 429 when exceeded.
3. **M-3 (eval)** ✅ — Replaced `eval(cmd.js)` with `new Function(cmd.js)()` to prevent scope leakage.
4. **M-4 (Observe WS auth)** ✅ — WebSocket upgrade now requires valid Vel cookie auth or Bearer token.
5. **L-1 (Cookie jar limit)** ✅ — Capped at 100 cookies per domain; oldest dropped when exceeded.
6. **L-2 (Screenshot cleanup)** ✅ — `os.RemoveAll()` on session end + periodic cleanup goroutine.
7. **L-3 (Error messages)** ✅ — All client-facing errors genericized; details logged server-side only.

---

*Report generated by Ram 🏹 — 2026-03-14*
