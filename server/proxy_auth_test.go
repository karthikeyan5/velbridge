package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyControlPlaneRequiresAuth(t *testing.T) {
	rl := NewFull(t.TempDir())
	sess := rl.proxySessions.GetOrCreateWithID("example.com", "session-1")

	tests := []struct {
		name string
		req  *http.Request
		run  func(http.ResponseWriter, *http.Request)
	}{
		{
			name: "sessions list",
			req:  httptest.NewRequest(http.MethodGet, "/bridge/proxy/_sessions", nil),
			run:  rl.HandleProxy,
		},
		{
			name: "latest by domain",
			req:  httptest.NewRequest(http.MethodGet, "/bridge/proxy/_latest?domain=example.com", nil),
			run:  rl.HandleProxy,
		},
		{
			name: "agent websocket",
			req:  httptest.NewRequest(http.MethodGet, "/bridge/proxy/_agent?session="+sess.ID, nil),
			run:  rl.HandleProxyAgentWS,
		},
		{
			name: "cookie import",
			req: httptest.NewRequest(
				http.MethodPost,
				"/bridge/proxy/_cookies",
				strings.NewReader(`{"domain":"example.com","cookies":"sid=abc"}`),
			),
			run: rl.HandleProxyCookieImport,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			tc.run(rr, tc.req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestProxyBrowserWSRequiresPerSessionToken(t *testing.T) {
	rl := NewFull(t.TempDir())
	sess := rl.proxySessions.GetOrCreateWithID("example.com", "session-1")

	missingTokenReq := httptest.NewRequest(http.MethodGet, "/bridge/proxy/_ws?session="+sess.ID, nil)
	rr := httptest.NewRecorder()
	rl.HandleProxyWS(rr, missingTokenReq)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	validTokenReq := httptest.NewRequest(http.MethodGet, "/bridge/proxy/_ws?session="+sess.ID+"&token="+sess.ControlToken, nil)
	if !rl.proxySessionTokenAuthorized(validTokenReq, sess.ID) {
		t.Fatal("valid per-session token was rejected")
	}

	wrongTokenReq := httptest.NewRequest(http.MethodGet, "/bridge/proxy/_ws?session="+sess.ID+"&token=wrong", nil)
	if rl.proxySessionTokenAuthorized(wrongTokenReq, sess.ID) {
		t.Fatal("wrong per-session token was accepted")
	}
}

func TestMonitorScriptCarriesEscapedProxyToken(t *testing.T) {
	script := buildMonitorScript(`sid"x`, `example.com`, `tok"<x>`)
	for _, want := range []string{
		`window.__velSession="sid\"x"`,
		`window.__velDomain="example.com"`,
		`window.__velProxyToken="tok\"\u003cx\u003e"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}
