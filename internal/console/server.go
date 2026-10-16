// Package console implements the browser management console as a thin Go
// client of the public cautem SDK. Authorization remains in the gateway.
package console

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cautem/quarshell-runtime/idp"
	cautem "github.com/cautem/quarshell-sdk/go/cautem"
)

const (
	sessionCookie = "cautem_console_session"
	pendingCookie = "cautem_console_login"
	maxFormBytes  = 64 << 10
	requestLimit  = 15 * time.Second
	sessionTTL    = 8 * time.Hour
	pendingTTL    = 10 * time.Minute
)

type Config struct {
	Listen    string
	PublicURL string
	Gateway   string
	Log       *slog.Logger
}

type Server struct {
	cfg       Config
	origin    *url.URL
	templates *template.Template
	log       *slog.Logger
	http      *http.Client
	mu        sync.Mutex
	sessions  map[string]session
	pending   map[string]pendingLogin
}

type session struct {
	Token        string
	RefreshToken string
	ExpiresAt    time.Time
	Deadline     time.Time
	CSRF         string
	OIDC         idp.PKCEConfig
}

type pendingLogin struct {
	Prepared idp.AuthorizationRequest
	OIDC     idp.PKCEConfig
	Browser  string
	Expires  time.Time
}

type oidcMetadata struct {
	Issuer            string `json:"issuer"`
	Audience          string `json:"audience"`
	ClientID          string `json:"client_id"`
	AllowInsecureHTTP bool   `json:"allow_insecure_http"`
}

type pageData struct {
	Viewer             map[string]any
	Overview           map[string]any
	Workspaces         []string
	Workspace          string
	Sandboxes          []cautem.Sandbox
	Selected           *cautem.Sandbox
	Logs               []cautem.LogLine
	LifecycleAvailable bool
	CSRF               string
	Error              string
}

func New(cfg Config) (*Server, error) {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8080"
	}
	if cfg.Gateway == "" {
		cfg.Gateway = "http://127.0.0.1:7443"
	}
	if cfg.PublicURL == "" {
		cfg.PublicURL = "http://" + cfg.Listen
	}
	origin, err := url.Parse(strings.TrimRight(cfg.PublicURL, "/"))
	originSchemeAllowed := err == nil && (origin.Scheme == "https" || origin.Scheme == "http" && isLoopback(origin.Hostname()))
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || !originSchemeAllowed || origin.Path != "" {
		return nil, fmt.Errorf("console public URL must be https or loopback http without a path")
	}
	gateway, err := url.Parse(strings.TrimRight(cfg.Gateway, "/"))
	gatewaySchemeAllowed := err == nil && (gateway.Scheme == "https" || gateway.Scheme == "http" && isLoopback(gateway.Hostname()))
	if err != nil || gateway.Host == "" || gateway.User != nil || gateway.RawQuery != "" || gateway.Fragment != "" || !gatewaySchemeAllowed || gateway.Path != "" {
		return nil, fmt.Errorf("console gateway URL must be https or loopback http without a path")
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	tmpl, err := template.New("console").Parse(pageTemplate)
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, origin: origin, templates: tmpl, log: cfg.Log,
		http: &http.Client{Timeout: 10 * time.Second}, sessions: map[string]session{}, pending: map[string]pendingLogin{}}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /assets/console.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = io.WriteString(w, pageCSS)
	})
	mux.HandleFunc("GET /login", s.login)
	mux.HandleFunc("GET /signed-out", s.signedOut)
	mux.HandleFunc("GET /auth/callback", s.callback)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("POST /sandboxes/action", s.sandboxAction)
	mux.HandleFunc("GET /", s.index)
	return s.securityHeaders(mux)
}

func (s *Server) Run(ctx context.Context) error {
	server := &http.Server{Addr: s.cfg.Listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return errors.Join(ctx.Err(), server.Shutdown(shutdownCtx))
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	workspace := cleanName(r.URL.Query().Get("workspace"))
	if workspace == "" {
		workspace = "default"
	}
	client := cautem.NewWithToken(s.cfg.Gateway, sess.Token)
	defer client.Close()
	client.Workspace = workspace
	ctx, cancel := context.WithTimeout(r.Context(), requestLimit)
	defer cancel()
	viewer, err := client.Whoami(ctx)
	if err != nil {
		s.renderError(w, http.StatusBadGateway, "gateway authentication failed")
		return
	}
	overview, err := client.ControlOverview(ctx)
	if err != nil {
		s.renderError(w, http.StatusBadGateway, "gateway overview unavailable")
		return
	}
	workspaceRecords, err := client.ListWorkspaces(ctx)
	if err != nil {
		s.renderError(w, http.StatusBadGateway, "workspace inventory unavailable")
		return
	}
	workspaces := make([]string, 0, len(workspaceRecords))
	for _, item := range workspaceRecords {
		if name := cleanName(item.Name); name != "" {
			workspaces = append(workspaces, name)
		}
	}
	sort.Strings(workspaces)
	rows, err := client.ListControlSandboxes(ctx, workspace, false)
	if err != nil {
		s.renderError(w, http.StatusBadGateway, "sandbox inventory unavailable")
		return
	}
	lifecycleAvailable, _ := overview["sandbox_lifecycle_available"].(bool)
	data := pageData{Viewer: viewer, Overview: overview, Workspaces: workspaces, Workspace: workspace, Sandboxes: rows, LifecycleAvailable: lifecycleAvailable, CSRF: sess.CSRF}
	if selectedName := cleanName(r.URL.Query().Get("sandbox")); selectedName != "" {
		selected, getErr := client.GetControlSandbox(ctx, workspace, selectedName)
		if getErr != nil {
			data.Error = "sandbox details unavailable"
		} else {
			data.Selected = &selected
			logs, logErr := client.GetControlSandboxLogs(ctx, workspace, selectedName, 120)
			if logErr != nil {
				data.Error = "sandbox logs unavailable"
			} else {
				data.Logs = logs
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := s.templates.ExecuteTemplate(w, "page", data); err != nil {
		s.log.Error("render console", "error", err)
	}
}

func (s *Server) sandboxAction(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(r)
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !s.validMutation(r, sess.CSRF) {
		http.Error(w, "request origin or CSRF token is invalid", http.StatusForbidden)
		return
	}
	workspace, name, action := cleanName(r.FormValue("workspace")), cleanName(r.FormValue("name")), r.FormValue("action")
	if workspace == "" {
		workspace = "default"
	}
	if name == "" {
		http.Error(w, "sandbox name required", http.StatusBadRequest)
		return
	}
	client := cautem.NewWithToken(s.cfg.Gateway, sess.Token)
	defer client.Close()
	ctx, cancel := context.WithTimeout(r.Context(), requestLimit)
	defer cancel()
	var (
		result cautem.ControlMutationResult
		err    error
	)
	switch action {
	case "create":
		image := strings.TrimSpace(r.FormValue("image"))
		if image == "" || len(image) > 1024 {
			http.Error(w, "sandbox image required", http.StatusBadRequest)
			return
		}
		result, err = client.CreateControlSandboxOperation(ctx, cautem.Sandbox{Workspace: workspace, Name: name, Image: image}, strings.Fields(r.FormValue("command")))
	case "start":
		result, err = client.ChangeControlSandboxStateOperation(ctx, workspace, name, "start")
	case "stop":
		result, err = client.ChangeControlSandboxStateOperation(ctx, workspace, name, "stop")
	case "delete":
		if r.FormValue("confirm") != "delete" {
			http.Error(w, "sandbox deletion requires confirmation", http.StatusBadRequest)
			return
		}
		result, err = client.DeleteControlSandboxOperation(ctx, workspace, name)
	default:
		http.Error(w, "unsupported action", http.StatusBadRequest)
		return
	}
	if err != nil && result.RequestID != "" {
		recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 3*time.Second)
		operation, recoveryErr := client.GetControlOperation(recoveryCtx, workspace, result.RequestID)
		recoveryCancel()
		if recoveryErr == nil && operation.State == "succeeded" {
			s.log.Info("console recovered completed sandbox action", "request_id", result.RequestID, "operation_id", operation.ID)
			err = nil
		} else if recoveryErr == nil {
			s.log.Warn("console recovered failed or pending sandbox action", "request_id", result.RequestID, "operation_id", operation.ID, "state", operation.State, "code", operation.ErrorCode)
		}
	}
	if err != nil {
		s.log.Warn("console sandbox action failed", "action", action, "workspace", workspace, "sandbox", name, "error", err)
		http.Error(w, "sandbox action failed", http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, "/?workspace="+url.QueryEscape(workspace)+"&sandbox="+url.QueryEscape(name), http.StatusSeeOther)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	meta, err := s.oidcMetadata(r.Context())
	if err == nil && meta.Issuer != "" && meta.ClientID != "" {
		cfg := idp.PKCEConfig{Issuer: meta.Issuer, ClientID: meta.ClientID, Audience: meta.Audience, Scopes: "profile", AllowInsecureHTTP: meta.AllowInsecureHTTP, HTTPClient: s.http}
		prepared, prepErr := idp.PrepareAuthorization(r.Context(), cfg, s.cfg.PublicURL+"/auth/callback")
		if prepErr != nil {
			s.renderError(w, http.StatusBadGateway, "OIDC login could not start")
			return
		}
		browser, randomErr := randomToken(24)
		if randomErr != nil {
			s.renderError(w, http.StatusInternalServerError, "login state could not be created")
			return
		}
		s.mu.Lock()
		s.pruneLocked(time.Now())
		s.pending[prepared.State] = pendingLogin{Prepared: prepared, OIDC: cfg, Browser: browser, Expires: time.Now().Add(pendingTTL)}
		s.mu.Unlock()
		s.setCookie(w, pendingCookie, browser, pendingTTL, true)
		http.Redirect(w, r, prepared.URL, http.StatusFound)
		return
	}
	if !isLoopback(s.origin.Hostname()) {
		s.renderError(w, http.StatusServiceUnavailable, "gateway OIDC is required for a non-loopback console")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestLimit)
	defer cancel()
	client := cautem.New(s.cfg.Gateway)
	token, loginErr := client.AuthLogin(ctx)
	_ = client.Close()
	if loginErr != nil {
		s.renderError(w, http.StatusBadGateway, "local gateway login failed")
		return
	}
	if err := s.establishSession(w, token, idp.TokenBundle{}, idp.PKCEConfig{}); err != nil {
		s.renderError(w, http.StatusInternalServerError, "session could not be created")
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	browserCookie, err := r.Cookie(pendingCookie)
	if state == "" || err != nil {
		http.Error(w, "invalid login callback", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	pending, ok := s.pending[state]
	delete(s.pending, state)
	s.mu.Unlock()
	if !ok || time.Now().After(pending.Expires) || subtle.ConstantTimeCompare([]byte(browserCookie.Value), []byte(pending.Browser)) != 1 {
		http.Error(w, "login state expired or does not match this browser", http.StatusBadRequest)
		return
	}
	if oidcErr := r.URL.Query().Get("error"); oidcErr != "" {
		http.Error(w, "identity provider rejected login", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestLimit)
	defer cancel()
	bundle, exchangeErr := idp.ExchangeAuthorizationCode(ctx, pending.OIDC, pending.Prepared, r.URL.Query().Get("code"))
	if exchangeErr != nil {
		s.renderError(w, http.StatusBadGateway, "OIDC token exchange failed")
		return
	}
	if err := s.establishSession(w, bundle.AccessToken, bundle, pending.OIDC); err != nil {
		s.renderError(w, http.StatusInternalServerError, "session could not be created")
		return
	}
	s.clearCookie(w, pendingCookie)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.session(r)
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !s.validMutation(r, sess.CSRF) {
		http.Error(w, "request origin or CSRF token is invalid", http.StatusForbidden)
		return
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.mu.Lock()
		delete(s.sessions, cookie.Value)
		s.mu.Unlock()
	}
	s.clearCookie(w, sessionCookie)
	http.Redirect(w, r, "/signed-out", http.StatusSeeOther)
}

func (s *Server) signedOut(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "signed_out", nil); err != nil {
		s.log.Error("render signed-out page", "error", err)
	}
}

func (s *Server) session(r *http.Request) (session, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return session{}, false
	}
	now := time.Now()
	s.mu.Lock()
	sess, ok := s.sessions[cookie.Value]
	if !ok || now.After(sess.Deadline) {
		delete(s.sessions, cookie.Value)
		s.mu.Unlock()
		return session{}, false
	}
	needsRefresh := !sess.ExpiresAt.IsZero() && now.After(sess.ExpiresAt.Add(-time.Minute)) && sess.RefreshToken != ""
	s.mu.Unlock()
	if !needsRefresh {
		return sess, true
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestLimit)
	defer cancel()
	bundle, refreshErr := idp.RefreshTokens(ctx, sess.OIDC, sess.RefreshToken)
	s.mu.Lock()
	defer s.mu.Unlock()
	current, stillPresent := s.sessions[cookie.Value]
	if refreshErr != nil {
		delete(s.sessions, cookie.Value)
		return session{}, false
	}
	if !stillPresent || current.RefreshToken != sess.RefreshToken {
		return current, stillPresent && time.Now().Before(current.Deadline)
	}
	current.Token = bundle.AccessToken
	if bundle.RefreshToken != "" {
		current.RefreshToken = bundle.RefreshToken
	}
	current.ExpiresAt = bundle.Expiry
	s.sessions[cookie.Value] = current
	return current, true
}

func (s *Server) establishSession(w http.ResponseWriter, token string, bundle idp.TokenBundle, cfg idp.PKCEConfig) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("empty access token")
	}
	id, err := randomToken(32)
	if err != nil {
		return err
	}
	csrf, err := randomToken(24)
	if err != nil {
		return err
	}
	sess := session{Token: token, RefreshToken: bundle.RefreshToken, ExpiresAt: bundle.Expiry, Deadline: time.Now().Add(sessionTTL), CSRF: csrf, OIDC: cfg}
	s.mu.Lock()
	s.pruneLocked(time.Now())
	s.sessions[id] = sess
	s.mu.Unlock()
	s.setCookie(w, sessionCookie, id, sessionTTL, true)
	return nil
}

func (s *Server) validMutation(r *http.Request, csrf string) bool {
	requestOrigin := r.Header.Get("Origin")
	originValid := requestOrigin == s.origin.String() || (requestOrigin == "" || requestOrigin == "null") && sameOriginReferer(r.Header.Get("Referer"), s.origin)
	if !originValid {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(r.FormValue("csrf")), []byte(csrf)) == 1
}

func sameOriginReferer(referer string, origin *url.URL) bool {
	parsed, err := url.Parse(referer)
	return err == nil && parsed.Scheme == origin.Scheme && strings.EqualFold(parsed.Host, origin.Host) && parsed.User == nil
}

func (s *Server) oidcMetadata(ctx context.Context) (oidcMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, requestLimit)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.cfg.Gateway, "/")+"/v1/auth/oidc", nil)
	if err != nil {
		return oidcMetadata{}, err
	}
	res, err := s.http.Do(req)
	if err != nil {
		return oidcMetadata{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return oidcMetadata{}, fmt.Errorf("OIDC metadata: %s", res.Status)
	}
	var metadata oidcMetadata
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&metadata); err != nil {
		return oidcMetadata{}, err
	}
	return metadata, nil
}

func (s *Server) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: int(ttl.Seconds()), HttpOnly: httpOnly, Secure: s.origin.Scheme == "https", SameSite: http.SameSiteLaxMode})
}

func (s *Server) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Path: "/", Expires: time.Unix(1, 0).UTC(), MaxAge: -1, HttpOnly: true, Secure: s.origin.Scheme == "https", SameSite: http.SameSiteLaxMode})
}

func (s *Server) pruneLocked(now time.Time) {
	for key, item := range s.pending {
		if now.After(item.Expires) {
			delete(s.pending, key)
		}
	}
	for key, item := range s.sessions {
		if now.After(item.Deadline) {
			delete(s.sessions, key)
		}
	}
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) renderError(w http.ResponseWriter, status int, message string) {
	s.log.Warn("console request failed", "status", status, "message", message)
	http.Error(w, message, status)
}

func cleanName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 253 {
		return ""
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return ""
		}
	}
	return value
}

func randomToken(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
