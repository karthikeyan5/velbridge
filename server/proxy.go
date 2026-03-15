package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	vel "vel/pkg/vel"
)

// ProxySession holds state for a single proxy-mode session.
type ProxySession struct {
	ID        string
	Domain    string
	CookieJar map[string][]*http.Cookie // domain → cookies
	CreatedAt time.Time
	mu        sync.Mutex
}

// proxyManager manages active proxy sessions.
type proxyManager struct {
	mu          sync.RWMutex
	sessions    map[string]*ProxySession
	rateLimiter *domainLimiter
}

func newProxyManager() *proxyManager {
	pm := &proxyManager{
		sessions:    make(map[string]*ProxySession),
		rateLimiter: newDomainLimiter(),
	}
	go pm.cleanup()
	return pm
}

func (pm *proxyManager) GetOrCreate(domain string) *ProxySession {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Use domain as session key for simplicity
	if s, ok := pm.sessions[domain]; ok {
		return s
	}
	s := &ProxySession{
		ID:        generateToken()[:12],
		Domain:    domain,
		CookieJar: make(map[string][]*http.Cookie),
		CreatedAt: time.Now(),
	}
	pm.sessions[domain] = s
	return s
}

// GetOrCreateWithID is like GetOrCreate but allows specifying a custom session ID.
func (pm *proxyManager) GetOrCreateWithID(domain, customID string) *ProxySession {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if s, ok := pm.sessions[domain]; ok {
		return s
	}
	id := customID
	if id == "" {
		id = generateToken()[:12]
	}
	s := &ProxySession{
		ID:        id,
		Domain:    domain,
		CookieJar: make(map[string][]*http.Cookie),
		CreatedAt: time.Now(),
	}
	pm.sessions[domain] = s
	return s
}

func (pm *proxyManager) Get(domain string) *ProxySession {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.sessions[domain]
}

func (pm *proxyManager) List() []*ProxySession {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	var out []*ProxySession
	for _, s := range pm.sessions {
		out = append(out, s)
	}
	return out
}

func (pm *proxyManager) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		pm.mu.Lock()
		now := time.Now()
		for k, s := range pm.sessions {
			if now.Sub(s.CreatedAt) > 24*time.Hour {
				delete(pm.sessions, k)
			}
		}
		pm.mu.Unlock()
	}
}

// isBlockedTarget returns true if the domain resolves to a private/local IP.
func isBlockedTarget(domain string) bool {
	// Block obvious local names
	lower := strings.ToLower(domain)
	if lower == "localhost" || lower == "0.0.0.0" || lower == "[::1]" {
		return true
	}

	// Resolve the domain
	addrs, err := net.LookupHost(domain)
	if err != nil {
		// Can't resolve → block to be safe
		return true
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if isBlockedIP(ip) {
			return true
		}
	}
	return false
}

// isBlockedIP checks if an IP address is private/reserved.
// Delegates to the framework's vel.IsPrivateOrReservedIP for comprehensive coverage.
func isBlockedIP(ip net.IP) bool {
	return vel.IsPrivateOrReservedIP(ip)
}

// safeDialContext returns a DialContext function that re-checks resolved IPs
// before connecting, preventing DNS rebinding / TOCTOU attacks.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("connection refused")
	}
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("connection refused")
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip != nil && isBlockedIP(ip) {
			return nil, fmt.Errorf("connection refused")
		}
	}
	// Connect using the first resolved IP to pin the resolution
	if len(ips) > 0 {
		addr = net.JoinHostPort(ips[0], port)
	}
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
}

// ── Per-domain rate limiter ──

const (
	rateLimitPerDomain = 10  // requests per second
	rateLimitBurst     = 20  // burst allowance
)

type domainLimiter struct {
	mu       sync.Mutex
	limiters map[string]*tokenBucket
}

type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

func newDomainLimiter() *domainLimiter {
	dl := &domainLimiter{limiters: make(map[string]*tokenBucket)}
	// Cleanup old entries periodically
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			dl.mu.Lock()
			now := time.Now()
			for k, tb := range dl.limiters {
				if now.Sub(tb.lastRefill) > 10*time.Minute {
					delete(dl.limiters, k)
				}
			}
			dl.mu.Unlock()
		}
	}()
	return dl
}

func (dl *domainLimiter) Allow(domain string) bool {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	tb, ok := dl.limiters[domain]
	if !ok {
		tb = &tokenBucket{
			tokens:     float64(rateLimitBurst),
			maxTokens:  float64(rateLimitBurst),
			refillRate: float64(rateLimitPerDomain),
			lastRefill: time.Now(),
		}
		dl.limiters[domain] = tb
	}

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}
	tb.lastRefill = now

	if tb.tokens < 1 {
		return false
	}
	tb.tokens--
	return true
}

// Cookie jar size limit per domain
const maxCookiesPerDomain = 100

// trimCookieJar caps the cookie jar at maxCookiesPerDomain, dropping oldest entries.
func trimCookieJar(jar []*http.Cookie) []*http.Cookie {
	if len(jar) <= maxCookiesPerDomain {
		return jar
	}
	// Keep the most recent cookies (end of slice)
	return jar[len(jar)-maxCookiesPerDomain:]
}

// extractProxyDomain extracts domain from /bridge/proxy/{domain}/{path...}
func extractProxyDomain(urlPath string) string {
	path := strings.TrimPrefix(urlPath, "/bridge/proxy/")
	if path == "" || path == urlPath {
		return ""
	}
	// Domain is up to the first slash (or end of string)
	idx := strings.Index(path, "/")
	if idx < 0 {
		return path
	}
	return path[:idx]
}

// extractProxyTargetPath extracts the target path from /bridge/proxy/{domain}/{path...}
func extractProxyTargetPath(urlPath string) string {
	path := strings.TrimPrefix(urlPath, "/bridge/proxy/")
	idx := strings.Index(path, "/")
	if idx < 0 {
		return "/"
	}
	return path[idx:]
}

// Regex patterns for HTML sanitization (kept for security/compatibility)
var (
	metaCSPRe       = regexp.MustCompile(`(?i)<meta[^>]+http-equiv\s*=\s*["']Content-Security-Policy["'][^>]*>`)
	integrityAttrRe = regexp.MustCompile(`\s+integrity\s*=\s*["'][^"']*["']`)
	baseTagRe       = regexp.MustCompile(`(?i)<base[^>]*>`)

	// Head-only rewriting: match <link> and <script> tags with src= or href= attributes
	// These fire before the Service Worker activates on first page load
	headLinkAbsRe      = regexp.MustCompile(`(<(?:link|script)\b[^>]*(?:src|href)\s*=\s*["'])https?://([^/"'\s]+)(/[^"'\s]*)?(["'][^>]*>)`)
	headLinkProtoRelRe = regexp.MustCompile(`(<(?:link|script)\b[^>]*(?:src|href)\s*=\s*["'])//([^/"'\s]+)(/[^"'\s]*)?(["'][^>]*>)`)
	headLinkRootRelRe  = regexp.MustCompile(`(<(?:link|script)\b[^>]*(?:src|href)\s*=\s*["'])(/[^/"'\s][^"'\s]*)(["'][^>]*>)`)
	headRe             = regexp.MustCompile(`(?is)(<head[^>]*>)(.*?)(</head>)`)
)

// sanitizeHTML strips meta CSP, integrity attributes, and base tags from HTML.
func sanitizeHTML(body string) string {
	body = metaCSPRe.ReplaceAllString(body, "")
	body = integrityAttrRe.ReplaceAllString(body, "")
	body = baseTagRe.ReplaceAllString(body, "")
	return body
}

// rewriteHeadURLs rewrites ONLY src= and href= in <link> and <script> tags
// within <head>...</head>. This is a safety net for first page load before
// the Service Worker activates.
func rewriteHeadURLs(body, domain string) string {
	return headRe.ReplaceAllStringFunc(body, func(headBlock string) string {
		m := headRe.FindStringSubmatch(headBlock)
		if len(m) < 4 {
			return headBlock
		}
		openTag, content, closeTag := m[1], m[2], m[3]

		// Rewrite absolute URLs in <link>/<script> tags
		content = headLinkAbsRe.ReplaceAllStringFunc(content, func(match string) string {
			sm := headLinkAbsRe.FindStringSubmatch(match)
			if len(sm) < 5 {
				return match
			}
			prefix, extDomain, path, suffix := sm[1], sm[2], sm[3], sm[4]
			if path == "" {
				path = "/"
			}
			return prefix + "/bridge/proxy/" + extDomain + path + suffix
		})

		// Rewrite protocol-relative URLs in <link>/<script> tags
		content = headLinkProtoRelRe.ReplaceAllStringFunc(content, func(match string) string {
			sm := headLinkProtoRelRe.FindStringSubmatch(match)
			if len(sm) < 5 {
				return match
			}
			prefix, extDomain, path, suffix := sm[1], sm[2], sm[3], sm[4]
			if path == "" {
				path = "/"
			}
			return prefix + "/bridge/proxy/" + extDomain + path + suffix
		})

		// Rewrite root-relative URLs in <link>/<script> tags
		content = headLinkRootRelRe.ReplaceAllStringFunc(content, func(match string) string {
			sm := headLinkRootRelRe.FindStringSubmatch(match)
			if len(sm) < 4 {
				return match
			}
			prefix, path, suffix := sm[1], sm[2], sm[3]
			if strings.HasPrefix(path, "/bridge/proxy/") {
				return match
			}
			return prefix + "/bridge/proxy/" + domain + path + suffix
		})

		return openTag + content + closeTag
	})
}

// swRegistrationScript returns the Service Worker registration <script> tag.
func swRegistrationScript() string {
	return `<script>
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('/bridge/proxy/_core/bridge/sw.js', {scope: '/bridge/proxy/'})
    .then(reg => { if (reg.active) return; reg.addEventListener('updatefound', () => { reg.installing.addEventListener('statechange', e => { if (e.target.state === 'activated') location.reload(); }); }); });
}
</script>`
}

// rewriteLocationHeader rewrites a Location header URL to stay within the proxy.
// Absolute URLs → /bridge/proxy/{domain}/{path}?{query}
// Protocol-relative → same treatment
// Root-relative → /bridge/proxy/{currentDomain}/{path}
func rewriteLocationHeader(loc, currentDomain string) string {
	loc = strings.TrimSpace(loc)
	if loc == "" {
		return loc
	}
	// Absolute URL: https://domain/path?query#frag
	if strings.HasPrefix(loc, "http://") || strings.HasPrefix(loc, "https://") {
		// Parse: strip scheme, extract domain and rest
		after := loc
		if strings.HasPrefix(after, "https://") {
			after = after[len("https://"):]
		} else {
			after = after[len("http://"):]
		}
		// Split domain from path+query+fragment
		slashIdx := strings.Index(after, "/")
		if slashIdx < 0 {
			return "/bridge/proxy/" + after + "/"
		}
		extDomain := after[:slashIdx]
		rest := after[slashIdx:] // includes path, query, fragment
		return "/bridge/proxy/" + extDomain + rest
	}
	// Protocol-relative: //domain/path
	if strings.HasPrefix(loc, "//") {
		after := loc[2:]
		slashIdx := strings.Index(after, "/")
		if slashIdx < 0 {
			return "/bridge/proxy/" + after + "/"
		}
		extDomain := after[:slashIdx]
		rest := after[slashIdx:]
		return "/bridge/proxy/" + extDomain + rest
	}
	// Root-relative: /path
	if strings.HasPrefix(loc, "/") {
		if strings.HasPrefix(loc, "/bridge/proxy/") {
			return loc // already rewritten
		}
		return "/bridge/proxy/" + currentDomain + loc
	}
	// Relative path — prefix with current domain
	return "/bridge/proxy/" + currentDomain + "/" + loc
}

func isHTMLResponse(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	return strings.Contains(ct, "text/html")
}

func isCSSResponse(resp *http.Response) bool {
	ct := resp.Header.Get("Content-Type")
	return strings.Contains(ct, "text/css")
}

// buildMonitorScript returns the JS to inject into proxied pages.
func buildMonitorScript(sessionID, domain string) string {
	return fmt.Sprintf(`<script src="/bridge/proxy/_core/bridge/html2canvas.min.js"></script>
<script src="/bridge/proxy/_core/bridge/monitor.js"></script>
<script src="/bridge/proxy/_core/bridge/commands.js"></script>
<script src="/bridge/proxy/_core/bridge/recorder.js"></script>
<script>window.__velSession="%s";window.__velDomain="%s";window.__velInit&&window.__velInit();</script>`, sessionID, domain)
}

func injectMonitorJS(body, sessionID, domain string) string {
	script := buildMonitorScript(sessionID, domain)
	swScript := swRegistrationScript()
	injection := swScript + script
	// Inject before </head> or at end of body
	idx := strings.Index(strings.ToLower(body), "</head>")
	if idx >= 0 {
		return body[:idx] + injection + body[idx:]
	}
	idx = strings.Index(strings.ToLower(body), "</body>")
	if idx >= 0 {
		return body[:idx] + injection + body[idx:]
	}
	return body + injection
}

// HandleProxy is the main reverse proxy handler for /bridge/proxy/
func (rl *Relay) HandleProxy(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Serve core assets
	if strings.HasPrefix(path, "/bridge/proxy/_core/") {
		rl.serveCoreAsset(w, r)
		return
	}

	// API: list proxy sessions
	if path == "/bridge/proxy/_sessions" {
		rl.handleProxySessions(w, r)
		return
	}

	// API: get latest session for a domain
	if path == "/bridge/proxy/_latest" {
		domain := r.URL.Query().Get("domain")
		if domain == "" {
			http.Error(w, "Missing domain parameter", 400)
			return
		}
		sess := rl.proxySessions.Get(domain)
		if sess == nil {
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "no session for domain", "domain": domain})
			return
		}
		browserConnected := rl.proxyWSClients.Get(sess.ID) != nil
		agentConnected := rl.proxyAgentClients.Get(sess.ID) != nil
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"session_id":        sess.ID,
			"domain":            sess.Domain,
			"browser_connected": browserConnected,
			"agent_connected":   agentConnected,
			"created_at":        sess.CreatedAt.Format(time.RFC3339),
		})
		return
	}

	domain := extractProxyDomain(path)
	if domain == "" {
		http.Error(w, "Missing domain in proxy URL", 400)
		return
	}

	// Security: block local/private targets
	if isBlockedTarget(domain) {
		http.Error(w, "Proxying to local/private networks is blocked", 403)
		return
	}

	// Rate limiting per domain
	if !rl.proxySessions.rateLimiter.Allow(domain) {
		http.Error(w, "Rate limit exceeded", 429)
		return
	}

	targetPath := extractProxyTargetPath(path)
	if r.URL.RawQuery != "" {
		targetPath += "?" + r.URL.RawQuery
	}

	// Allow fixed session ID via _vs query param
	customSessionID := r.URL.Query().Get("_vs")
	var sess *ProxySession
	if customSessionID != "" {
		sess = rl.proxySessions.GetOrCreateWithID(domain, customSessionID)
	} else {
		sess = rl.proxySessions.GetOrCreate(domain)
	}

	// Use a transport with safe dialer to prevent DNS rebinding
	safeTransport := &http.Transport{
		DialContext: safeDialContext,
	}

	proxy := &httputil.ReverseProxy{
		Transport: safeTransport,
		Director: func(req *http.Request) {
			req.URL.Scheme = "https"
			req.URL.Host = domain
			req.URL.Path = extractProxyTargetPath(req.URL.Path)
			req.URL.RawQuery = r.URL.RawQuery
			req.Host = domain

			// Set Origin/Referer to original domain
			req.Header.Set("Origin", "https://"+domain)
			req.Header.Set("Referer", "https://"+domain+req.URL.Path)

			// Remove Accept-Encoding so we get uncompressed content to rewrite
			req.Header.Del("Accept-Encoding")

			// Strip sensitive headers from user's request — don't forward
			// the user's own auth cookies/tokens to the target site
			req.Header.Del("Cookie")
			req.Header.Del("Authorization")
			req.Header.Del("X-Forwarded-For")
			req.Header.Del("X-Real-Ip")

			// Attach cookies from server-side jar ONLY
			sess.mu.Lock()
			for _, c := range sess.CookieJar[domain] {
				req.AddCookie(c)
			}
			sess.mu.Unlock()
		},
		ModifyResponse: func(resp *http.Response) error {
			// Capture Set-Cookie → server-side jar (capped at maxCookiesPerDomain)
			if cookies := resp.Header["Set-Cookie"]; len(cookies) > 0 {
				sess.mu.Lock()
				for _, raw := range cookies {
					header := http.Header{"Set-Cookie": {raw}}
					parsed := (&http.Response{Header: header}).Cookies()
					sess.CookieJar[domain] = append(sess.CookieJar[domain], parsed...)
				}
				sess.CookieJar[domain] = trimCookieJar(sess.CookieJar[domain])
				sess.mu.Unlock()
			}

			// Strip security headers
			resp.Header.Del("Content-Security-Policy")
			resp.Header.Del("X-Frame-Options")
			resp.Header.Del("Cross-Origin-Opener-Policy")
			resp.Header.Del("Cross-Origin-Embedder-Policy")
			resp.Header.Del("Cross-Origin-Resource-Policy")

			// Rewrite Location header on redirects so the browser stays in proxy
			if loc := resp.Header.Get("Location"); loc != "" {
				resp.Header.Set("Location", rewriteLocationHeader(loc, domain))
			}

			if isHTMLResponse(resp) {
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					return err
				}
				bodyStr := string(body)
				// Sanitize: strip meta CSP, integrity attrs, base tags
				bodyStr = sanitizeHTML(bodyStr)
				// Minimal head-only rewrite: covers <link>/<script> in <head>
				// before SW activates on first load
				bodyStr = rewriteHeadURLs(bodyStr, domain)
				// Inject SW registration + monitor scripts
				bodyStr = injectMonitorJS(bodyStr, sess.ID, domain)

				resp.Body = io.NopCloser(strings.NewReader(bodyStr))
				resp.ContentLength = -1           // Use chunked transfer — avoids HTTP2 mismatch with downstream compression
				resp.Header.Del("Content-Length")  // Let transport set it or use chunked
				resp.Header.Del("Content-Encoding")
			}
			// CSS: no longer rewritten — Service Worker handles external CSS URLs
			// Non-HTML/CSS: stream through unchanged
			// Still remove Content-Length to prevent HTTP/2 protocol errors
			// when downstream proxy compresses or re-frames the response
			resp.ContentLength = -1
			resp.Header.Del("Content-Length")

			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			vel.SafeHTTPError(w, r, 502, "Proxy error: unable to reach target", fmt.Errorf("proxying %s: %w", domain, err))
		},
	}

	proxy.ServeHTTP(w, r)
}

// serveCoreAsset serves vendored JS files from the app's core/bridge/ directory.
func (rl *Relay) serveCoreAsset(w http.ResponseWriter, r *http.Request) {
	// /bridge/proxy/_core/bridge/filename.js → core/bridge/filename.js
	raw := strings.TrimPrefix(r.URL.Path, "/bridge/proxy/_core/")
	relPath, err := vel.SafeRelPath(raw)
	if err != nil {
		http.Error(w, "Not found", 404)
		return
	}

	fullPath := rl.appDir + "/core/" + relPath
	if strings.HasSuffix(relPath, ".js") {
		w.Header().Set("Content-Type", "application/javascript")
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, fullPath)
}

// serveDiffPage serves the visual diff page.
// The HTML reads query params via JavaScript, so we just serve the static file.
func (rl *Relay) serveDiffPage(w http.ResponseWriter, r *http.Request) {
	diffFile := rl.appDir + "/pages/diff/index.html"
	data, err := os.ReadFile(diffFile)
	if err != nil {
		http.Error(w, "Diff page not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// sanitizeDiffURL validates diff URLs — only allows http(s) and relative paths.
// Blocks javascript:, data:, file:, and other dangerous schemes.
func sanitizeDiffURL(u string) string {
	if u == "" {
		return ""
	}
	u = strings.TrimSpace(u)
	lower := strings.ToLower(u)
	// Allow relative paths starting with /
	if strings.HasPrefix(u, "/") {
		return u
	}
	// Allow http and https
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return u
	}
	// Block everything else (javascript:, data:, file:, etc.)
	return ""
}

// htmlEscape escapes HTML special characters to prevent XSS.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func mustOpen(path string) io.ReadCloser {
	f, err := http.Dir("/").Open(path)
	if err != nil {
		return io.NopCloser(strings.NewReader(""))
	}
	return f
}

// handleProxySessions returns active proxy sessions as JSON.
func (rl *Relay) handleProxySessions(w http.ResponseWriter, r *http.Request) {
	sessions := rl.proxySessions.List()
	type sessionInfo struct {
		ID               string `json:"id"`
		Domain           string `json:"domain"`
		CreatedAt        string `json:"created_at"`
		BrowserConnected bool   `json:"browser_connected"`
		AgentConnected   bool   `json:"agent_connected"`
	}
	out := make([]sessionInfo, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, sessionInfo{
			ID:               s.ID,
			Domain:           s.Domain,
			CreatedAt:        s.CreatedAt.Format(time.RFC3339),
			BrowserConnected: rl.proxyWSClients.Get(s.ID) != nil,
			AgentConnected:   rl.proxyAgentClients.Get(s.ID) != nil,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"sessions": out})
}

// HandleDiffPage serves the visual diff page at /bridge/diff/.
func (rl *Relay) HandleDiffPage(w http.ResponseWriter, r *http.Request) {
	rl.serveDiffPage(w, r)
}

// HandleBridgeConnect serves the pairing/connect page at /bridge/debug/connect.
func (rl *Relay) HandleBridgeConnect(w http.ResponseWriter, r *http.Request) {
	connectFile := rl.appDir + "/pages/relay-connect/index.html"
	http.ServeFile(w, r, connectFile)
}

// HandleProxyCookieImport handles cookie import from the OAuth flow.
func (rl *Relay) HandleProxyCookieImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	var body struct {
		Domain  string `json:"domain"`
		Cookies string `json:"cookies"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad request", 400)
		return
	}

	sess := rl.proxySessions.Get(body.Domain)
	if sess == nil {
		http.Error(w, "No proxy session for domain", 404)
		return
	}

	// Parse cookies from "name=value" lines (capped at maxCookiesPerDomain)
	sess.mu.Lock()
	for _, line := range strings.Split(body.Cookies, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			sess.CookieJar[body.Domain] = append(sess.CookieJar[body.Domain], &http.Cookie{
				Name:  strings.TrimSpace(parts[0]),
				Value: strings.TrimSpace(parts[1]),
			})
		}
	}
	sess.CookieJar[body.Domain] = trimCookieJar(sess.CookieJar[body.Domain])
	sess.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
