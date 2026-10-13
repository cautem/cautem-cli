package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cautem/cautem-cli/internal/storage/gwconfig"
	display "github.com/cautem/cautem-display"
	"github.com/cautem/cautem-runtime/idp"
)

// GatewayLoginInteractive stores a bearer token.
// When the current gateway has oidc_issuer, runs Authorization Code + PKCE against the IdP
// (OpenShell). Otherwise falls back to local-dev /v1/auth/login (browser redirect or direct).
func (a *App) GatewayLoginInteractive(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("CAUTEM_GATEWAY_TOKEN"))
	}
	if token != "" {
		return a.GatewayLogin(token)
	}
	cfg, _, err := gwconfig.Load()
	if err != nil {
		return err
	}
	if cfg.Current == "" {
		return fmt.Errorf("no gateway selected")
	}
	g := cfg.Gateways[cfg.Current]

	issuer := strings.TrimSpace(g.OIDCIssuer)
	clientID := strings.TrimSpace(g.OIDCClientID)
	audience := strings.TrimSpace(g.OIDCAudience)
	scopes := strings.TrimSpace(g.OIDCScopes)
	allowHTTP := g.OIDCAllowHTTP
	if issuer == "" {
		if meta, err := fetchOIDCMeta(g.URL); err == nil {
			issuer = meta.Issuer
			if clientID == "" {
				clientID = meta.ClientID
			}
			if audience == "" {
				audience = meta.Audience
			}
			allowHTTP = allowHTTP || meta.AllowInsecureHTTP
		}
	}
	if issuer != "" {
		if clientID == "" {
			return fmt.Errorf("gateway login: OIDC issuer set but client_id missing (gateway add --oidc-client-id or CAUTEM_OIDC_CLIENT_ID on gateway)")
		}
		ctx, cancel := a.withTimeout(TimeoutWaitLong)
		defer cancel()
		bundle, err := idp.BrowserPKCE(ctx, idp.PKCEConfig{
			Issuer:            issuer,
			ClientID:          clientID,
			Audience:          audience,
			Scopes:            scopes,
			AllowInsecureHTTP: allowHTTP,
			OpenURL:           openBrowser,
			RedirectPort:      18765, // fixed for Dex/Keycloak static redirect allowlists
		})
		if err != nil {
			return fmt.Errorf("gateway login oidc: %w", err)
		}
		return a.storeOIDCBundle(cfg.Current, bundle)
	}

	u, err := a.currentGatewayURL()
	if err != nil {
		return err
	}
	tok, err := a.gatewayLoginBrowserLocal(u)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway login: browser flow failed (%v); trying direct local-dev auth\n", err)
		ctx, cancel := a.withTimeout(TimeoutAPI)
		defer cancel()
		tok, err = a.clientFor(u).AuthLogin(ctx)
		if err != nil {
			return fmt.Errorf("gateway login: %w", err)
		}
	}
	return a.GatewayLogin(tok)
}

func (a *App) storeOIDCBundle(name string, bundle idp.TokenBundle) error {
	cfg, _, err := gwconfig.Load()
	if err != nil {
		return err
	}
	g := cfg.Gateways[name]
	g.Token = bundle.AccessToken
	g.RefreshToken = bundle.RefreshToken
	if !bundle.Expiry.IsZero() {
		g.TokenExpiresAtMS = bundle.Expiry.UnixMilli()
	} else {
		g.TokenExpiresAtMS = 0
	}
	cfg.Gateways[name] = g
	if err := gwconfig.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("logged in to gateway %s via OIDC\n", name)
	return nil
}

type oidcMeta struct {
	Issuer            string `json:"issuer"`
	Audience          string `json:"audience"`
	ClientID          string `json:"client_id"`
	AllowInsecureHTTP bool   `json:"allow_insecure_http"`
}

func fetchOIDCMeta(gatewayURL string) (oidcMeta, error) {
	ctx, cancel := context.WithTimeout(context.Background(), TimeoutAPIShort)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(gatewayURL, "/")+"/v1/auth/oidc", nil)
	if err != nil {
		return oidcMeta{}, err
	}
	res, err := (&http.Client{Timeout: TimeoutAPI}).Do(req)
	if err != nil {
		return oidcMeta{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return oidcMeta{}, fmt.Errorf("%s", res.Status)
	}
	var m oidcMeta
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		return oidcMeta{}, err
	}
	return m, nil
}

func (a *App) gatewayLoginBrowserLocal(gatewayURL string) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	loginURL := gatewayURL + "/v1/auth/login?redirect_uri=" + url.QueryEscape(redirect)

	type result struct {
		token string
		err   error
	}
	ch := make(chan result, 1)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}
			tok := strings.TrimSpace(r.URL.Query().Get("token"))
			if tok == "" {
				http.Error(w, "missing token", http.StatusBadRequest)
				ch <- result{err: fmt.Errorf("callback missing token")}
				return
			}
			fmt.Fprint(w, "cautem gateway login ok — you can close this tab")
			ch <- result{token: tok}
		}),
		ReadHeaderTimeout: loginHeaderReadTimeout,
	}
	go func() {
		_ = srv.Serve(ln)
	}()
	defer func() {
		ctx, cancel := a.withTimeout(TimeoutProbe)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if err := openBrowser(loginURL); err != nil {
		fmt.Fprintf(os.Stderr, "open browser: %v\n", err)
		fmt.Fprintf(os.Stderr, "visit: %s\n", loginURL)
	}
	select {
	case res := <-ch:
		return res.token, res.err
	case <-time.After(loginTimeout):
		return "", fmt.Errorf("timed out waiting for login callback")
	}
}

func openBrowser(u string) error {
	return display.OpenHostBrowser(u)
}
