package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cautem/cautem-runtime/idp"
)

func TestNewRejectsPublicNetworkHTTP(t *testing.T) {
	if _, err := New(Config{PublicURL: "http://console.example.com", Gateway: "https://gateway.example.com"}); err == nil {
		t.Fatal("expected public HTTP console URL rejection")
	}
	if _, err := New(Config{PublicURL: "https://console.example.com/path", Gateway: "https://gateway.example.com"}); err == nil {
		t.Fatal("expected public URL path rejection")
	}
}

func TestNewRejectsURLCredentialsAndDecorations(t *testing.T) {
	for _, tc := range []struct {
		name, publicURL, gateway string
	}{
		{name: "public userinfo", publicURL: "https://user:pass@console.example.com", gateway: "https://gateway.example.com"},
		{name: "public query", publicURL: "https://console.example.com?token=secret", gateway: "https://gateway.example.com"},
		{name: "gateway userinfo", publicURL: "https://console.example.com", gateway: "https://user:pass@gateway.example.com"},
		{name: "gateway query", publicURL: "https://console.example.com", gateway: "https://gateway.example.com?token=secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(Config{PublicURL: tc.publicURL, Gateway: tc.gateway}); err == nil {
				t.Fatal("expected decorated URL rejection")
			}
		})
	}
}

func TestSecurityHeadersAndLogoutCSRF(t *testing.T) {
	server, err := New(Config{PublicURL: "http://127.0.0.1:8080", Gateway: "http://127.0.0.1:7443"})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := server.establishSession(recorder, "token", idpTokenBundle(), idpConfig()); err != nil {
		t.Fatal(err)
	}
	cookie := recorder.Result().Cookies()[0]
	server.mu.Lock()
	sess := server.sessions[cookie.Value]
	server.mu.Unlock()

	bad := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(url.Values{"csrf": {sess.CSRF}}.Encode()))
	bad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	bad.AddCookie(cookie)
	badRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(badRecorder, bad)
	if badRecorder.Code != http.StatusForbidden {
		t.Fatalf("bad origin status=%d", badRecorder.Code)
	}
	if badRecorder.Header().Get("Content-Security-Policy") == "" || badRecorder.Header().Get("X-Frame-Options") != "DENY" || badRecorder.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatal("security headers are missing")
	}

	good := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(url.Values{"csrf": {sess.CSRF}}.Encode()))
	good.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	good.Header.Set("Origin", "http://127.0.0.1:8080")
	good.AddCookie(cookie)
	goodRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(goodRecorder, good)
	if goodRecorder.Code != http.StatusSeeOther {
		t.Fatalf("logout status=%d body=%q", goodRecorder.Code, goodRecorder.Body.String())
	}
	if got := goodRecorder.Header().Get("Location"); got != "/signed-out" {
		t.Fatalf("logout location=%q, want /signed-out", got)
	}
	clearedCookie := goodRecorder.Header().Get("Set-Cookie")
	if !strings.Contains(clearedCookie, "Max-Age=0") || !strings.Contains(clearedCookie, "Expires=Thu, 01 Jan 1970") {
		t.Fatalf("logout cookie expiry=%q; want Max-Age=0 and an expired Expires value", clearedCookie)
	}
	server.mu.Lock()
	_, exists := server.sessions[cookie.Value]
	server.mu.Unlock()
	if exists {
		t.Fatal("logout did not remove server-side session")
	}

	request := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(url.Values{"csrf": {sess.CSRF}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "null")
	request.Header.Set("Referer", "http://127.0.0.1:8080/")
	if err := request.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if !server.validMutation(request, sess.CSRF) {
		t.Fatal("same-origin referer fallback rejected a valid browser mutation")
	}
	request.Header.Set("Referer", "http://attacker.example/")
	if server.validMutation(request, sess.CSRF) {
		t.Fatal("foreign referer passed mutation validation")
	}
}

func TestOIDCPKCELoginCreatesServerSideSession(t *testing.T) {
	var issuerURL string
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": issuerURL, "authorization_endpoint": issuerURL + "/authorize", "token_endpoint": issuerURL + "/token"})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("code") != "code-1" || r.Form.Get("code_verifier") == "" {
				t.Fatalf("unexpected token request: %v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-1", "refresh_token": "refresh-1", "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(issuer.Close)
	issuerURL = issuer.URL

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/oidc" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuerURL, "client_id": "console", "audience": "cautem", "allow_insecure_http": true})
	}))
	t.Cleanup(gateway.Close)

	server, err := New(Config{PublicURL: "http://127.0.0.1:8080", Gateway: gateway.URL})
	if err != nil {
		t.Fatal(err)
	}
	loginRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	if loginRecorder.Code != http.StatusFound {
		t.Fatalf("login status=%d body=%q", loginRecorder.Code, loginRecorder.Body.String())
	}
	location, err := url.Parse(loginRecorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	pendingCookie := findCookie(t, loginRecorder.Result().Cookies(), pendingCookie)
	callback := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+url.QueryEscape(location.Query().Get("state"))+"&code=code-1", nil)
	callback.AddCookie(pendingCookie)
	callbackRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(callbackRecorder, callback)
	if callbackRecorder.Code != http.StatusSeeOther {
		t.Fatalf("callback status=%d body=%q", callbackRecorder.Code, callbackRecorder.Body.String())
	}
	sessionCookie := findCookie(t, callbackRecorder.Result().Cookies(), sessionCookie)
	server.mu.Lock()
	sess, exists := server.sessions[sessionCookie.Value]
	server.mu.Unlock()
	if !exists || sess.Token != "access-1" || sess.RefreshToken != "refresh-1" || sess.CSRF == "" {
		t.Fatalf("unexpected session: exists=%v value=%+v", exists, sess)
	}
}

func findCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.MaxAge > 0 {
			return cookie
		}
	}
	t.Fatalf("cookie %q not found", name)
	return nil
}

func idpTokenBundle() idp.TokenBundle { return idp.TokenBundle{} }

func idpConfig() idp.PKCEConfig { return idp.PKCEConfig{} }
