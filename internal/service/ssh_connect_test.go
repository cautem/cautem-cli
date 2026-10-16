package service

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	datamodelv1 "github.com/NVIDIA/OpenShell/sdk/go/proto/datamodelv1"
	openshellv1 "github.com/NVIDIA/OpenShell/sdk/go/proto/openshellv1"
	"github.com/cautem/quarshell-cli/internal/storage/gwconfig"
	"github.com/cautem/quarshell-sdk/go/cautem"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type sshTestServer struct {
	openshellv1.UnimplementedOpenShellServer
	calls, unauth *atomic.Int32
}

func (s *sshTestServer) GetSandbox(context.Context, *openshellv1.GetSandboxRequest) (*openshellv1.SandboxResponse, error) {
	return &openshellv1.SandboxResponse{Sandbox: &openshellv1.Sandbox{Metadata: &datamodelv1.ObjectMeta{Id: "id-box", Name: "box"}}}, nil
}
func (s *sshTestServer) CreateSshSession(ctx context.Context, _ *openshellv1.CreateSshSessionRequest) (*openshellv1.CreateSshSessionResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if len(md.Get("authorization")) == 0 || md.Get("authorization")[0] != "Bearer user-tok" {
		s.unauth.Add(1)
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	if s.calls.Add(1) < 3 {
		return nil, status.Error(codes.FailedPrecondition, "sandbox is not ready (supervisor relay not connected)")
	}
	return &openshellv1.CreateSshSessionResponse{SandboxId: "id-box", Token: "sess-tok", GatewayHost: "127.0.0.1", GatewayPort: 2222, GatewayScheme: "http"}, nil
}

// isolate points config/state lookups at a temp dir and clears gateway env.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("HOME", dir)
	for _, k := range []string{cautem.EnvToken, "OPENSHELL_GATEWAY", "CAUTEM_GATEWAY_URL"} {
		t.Setenv(k, "")
	}
	return dir
}

func saveGateways(t *testing.T, current string, gws map[string]gwconfig.Gateway) {
	t.Helper()
	if err := gwconfig.Save(gwconfig.File{Current: current, Gateways: gws}); err != nil {
		t.Fatal(err)
	}
}

func TestSSHCommandArgs(t *testing.T) {
	pc := "/usr/bin/cautem ssh-proxy --gateway-name dev --name box"

	got := SSHCommandArgs(pc, "cautem-box", true, nil)
	joined := strings.Join(got, " ")
	for _, want := range []string{
		"-o StrictHostKeyChecking=no",
		"-o UserKnownHostsFile=/dev/null",
		"-o ForwardAgent=no",
		"-o ForwardX11=no",
		"-o ProxyCommand=" + pc,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	if got[len(got)-1] != "cautem-box" {
		t.Fatalf("interactive: alias must be last, got %q", got)
	}
	for _, a := range got {
		if a == "-t" || a == "-T" {
			t.Fatalf("interactive shell must not force tty flags: %q", got)
		}
	}

	got = SSHCommandArgs(pc, "cautem-box", true, []string{"ls", "-la"})
	if n := len(got); got[n-3] != "-t" || got[n-2] != "cautem-box" || got[n-1] != "ls -la" {
		t.Fatalf("tty command argv: %q", got)
	}
	got = SSHCommandArgs(pc, "cautem-box", false, []string{"echo", "a b"})
	if n := len(got); got[n-3] != "-T" || got[n-1] != "echo 'a b'" {
		t.Fatalf("non-tty command argv: %q", got)
	}
}

func TestPosixQuoteRoundTrip(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	for _, in := range []string{"plain", "", "a b", "it's", `"dq"`, "$HOME", "`id`", "a;rm -rf /", "x\ny", `back\slash`, "*", "~"} {
		out, err := exec.Command(sh, "-c", "printf %s "+posixQuote(in)).Output()
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if string(out) != in {
			t.Fatalf("posixQuote(%q) round-trip = %q", in, out)
		}
	}
}

func TestEditorCommand(t *testing.T) {
	cases := map[string][]string{
		"cursor": {"cursor", "--remote", "ssh-remote+cautem-box", "/workspace"},
		"vscode": {"code", "--remote", "ssh-remote+cautem-box", "/workspace"},
		"code":   {"code", "--remote", "ssh-remote+cautem-box", "/workspace"},
	}
	for ed, want := range cases {
		got, err := EditorCommand(ed, "cautem-box")
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("EditorCommand(%q) = %q, %v; want %q", ed, got, err, want)
		}
	}
	if _, err := EditorCommand("vim", "x"); err == nil {
		t.Fatal("unknown editor accepted")
	}
}

func TestParseForwardBind(t *testing.T) {
	ok := map[string]struct {
		bind string
		port int
	}{
		"8080":           {"127.0.0.1", 8080},
		":8080":          {"127.0.0.1", 8080},
		"0.0.0.0:3000":   {"0.0.0.0", 3000},
		"localhost:22":   {"localhost", 22},
		"[::1]:9000":     {"::1", 9000},
		" 127.0.0.1:1 ":  {"127.0.0.1", 1},
		"10.0.0.5:65535": {"10.0.0.5", 65535},
	}
	for in, want := range ok {
		b, p, err := ParseForwardBind(in)
		if err != nil || b != want.bind || p != want.port {
			t.Fatalf("ParseForwardBind(%q) = %q,%d,%v want %q,%d", in, b, p, err, want.bind, want.port)
		}
	}
	for _, in := range []string{"", "0", "65536", "-1", "abc", "host.example:80", "1.2.3.4:x"} {
		if _, _, err := ParseForwardBind(in); err == nil {
			t.Fatalf("ParseForwardBind(%q) accepted", in)
		}
	}
}

func TestProxyGatewayArgsAndHostBlock(t *testing.T) {
	isolate(t)
	saveGateways(t, "dev", map[string]gwconfig.Gateway{
		"dev":  {URL: "http://127.0.0.1:7443/"},
		"prod": {URL: "https://gw.example.com"},
	})

	a := New()
	got, err := a.proxyGatewayArgs()
	if err != nil || !reflect.DeepEqual(got, []string{"--gateway-name", "dev"}) {
		t.Fatalf("current gateway: %q, %v", got, err)
	}

	a.GatewayNameOverride = "prod"
	got, err = a.proxyGatewayArgs()
	if err != nil || !reflect.DeepEqual(got, []string{"--gateway-name", "prod"}) {
		t.Fatalf("-g override: %q, %v", got, err)
	}

	a = New()
	a.GatewayURLOverride = "https://adhoc.example.com"
	got, err = a.proxyGatewayArgs()
	if err != nil || !reflect.DeepEqual(got, []string{"--server", "https://adhoc.example.com"}) {
		t.Fatalf("unknown URL must fall back to --server: %q, %v", got, err)
	}

	a = New()
	block, err := a.SandboxSSHHostBlock("box")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(block, "Host cautem-box\n") {
		t.Fatalf("host block: %q", block)
	}
	if !strings.Contains(block, "ssh-proxy --gateway-name dev --name box") {
		t.Fatalf("ProxyCommand must reference the config gateway, not a token: %q", block)
	}
	if strings.Contains(strings.ToLower(block), "token") {
		t.Fatalf("host block must never embed tokens: %q", block)
	}
}

func TestResolveGatewayURL(t *testing.T) {
	isolate(t)
	saveGateways(t, "dev", map[string]gwconfig.Gateway{"dev": {URL: "http://127.0.0.1:7443"}})
	a := New()
	cases := []struct{ url, name, want string }{
		{"https://x.example/", "", "https://x.example"},
		{"", "dev", "http://127.0.0.1:7443"},
		{"", "http://127.0.0.1:9999/", "http://127.0.0.1:9999"},
		{"", "", "http://127.0.0.1:7443"},
	}
	for _, c := range cases {
		got, err := a.resolveGatewayURL(c.url, c.name)
		if err != nil || got != c.want {
			t.Fatalf("resolveGatewayURL(%q,%q) = %q, %v want %q", c.url, c.name, got, err, c.want)
		}
	}
	if _, err := a.resolveGatewayURL("", "missing"); err == nil {
		t.Fatal("unknown gateway name accepted")
	}
}

func TestGatewayTokenForURL(t *testing.T) {
	dir := isolate(t)
	custom := filepath.Join(dir, "gwdata")
	if err := os.MkdirAll(custom, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(custom, gatewayAuthTokenFile), []byte("file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saveGateways(t, "a", map[string]gwconfig.Gateway{
		"a": {URL: "https://a.example", Token: "cfg-token"},
		"b": {URL: "https://b.example", DataDir: custom},
		"c": {URL: "https://c.example"},
	})
	a := New()

	if got := a.gatewayTokenForURL("https://a.example/"); got != "cfg-token" {
		t.Fatalf("config token: %q", got)
	}
	if got := a.gatewayTokenForURL("https://b.example"); got != "file-token" {
		t.Fatalf("data_dir auth_token: %q", got)
	}
	if got := a.gatewayTokenForURL("https://c.example"); got != "" {
		t.Fatalf("remote gateway without token must not read local files: %q", got)
	}

	local := defaultGatewayDataDir()
	if !strings.HasPrefix(local, dir) {
		t.Fatalf("default data dir %q not under XDG_STATE_HOME", local)
	}
	if err := os.MkdirAll(local, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, gatewayAuthTokenFile), []byte("local-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := a.gatewayTokenForURL(localGatewayURL); got != "local-token" {
		t.Fatalf("local gateway auth_token: %q", got)
	}

	t.Setenv(cautem.EnvToken, "env-token")
	if got := a.gatewayTokenForURL("https://a.example"); got != "env-token" {
		t.Fatalf("env must win: %q", got)
	}
}

func TestCreateSSHSessionWait(t *testing.T) {
	var calls, unauth atomic.Int32
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	openshellv1.RegisterOpenShellServer(srv, &sshTestServer{calls: &calls, unauth: &unauth})
	go func() { _ = srv.Serve(listener) }()
	defer srv.Stop()
	base := "http://" + listener.Addr().String()
	c := cautem.NewWithToken(base, "user-tok")
	sess, err := createSSHSessionWait(context.Background(), c, "box", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Token != "sess-tok" || sess.SandboxID != "id-box" || calls.Load() != 3 || unauth.Load() != 0 {
		t.Fatalf("sess=%+v calls=%d unauth=%d", sess, calls.Load(), unauth.Load())
	}

	calls.Store(0)
	_, err = createSSHSessionWait(context.Background(), c, "box", 0)
	if !errors.Is(err, cautem.ErrSandboxNotReady) {
		t.Fatalf("timeout must surface ErrSandboxNotReady, got %v", err)
	}

	bad := cautem.NewWithToken(base, "wrong")
	if _, err := createSSHSessionWait(context.Background(), bad, "box", 10*time.Second); err == nil || errors.Is(err, cautem.ErrSandboxNotReady) {
		t.Fatalf("401 must fail fast, got %v", err)
	}
}

func TestBridgeStdio(t *testing.T) {
	a, b := net.Pipe()
	go func() {
		buf := make([]byte, 5)
		_, _ = b.Read(buf)
		_, _ = b.Write([]byte(strings.ToUpper(string(buf))))
		_ = b.Close()
	}()
	var out strings.Builder
	if err := bridgeStdio(context.Background(), a, strings.NewReader("hello"), &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "HELLO" {
		t.Fatalf("bridged %q", out.String())
	}

	c, d := net.Pipe()
	defer d.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	pr, pw := net.Pipe()
	defer pw.Close()
	go func() { done <- bridgeStdio(ctx, c, pr, &out) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancel must end cleanly: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bridgeStdio ignored cancel")
	}
}
