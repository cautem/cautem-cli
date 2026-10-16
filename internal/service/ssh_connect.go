package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cautem/cautem-core/defaults"
	"github.com/cautem/cautem-core/relayproto"
	"github.com/cautem/cautem-sdk/go/cautem"
	"github.com/cautem/quarshell-cli/internal/sshconfig"
	"github.com/cautem/quarshell-cli/internal/storage/gwconfig"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// sessionReadyTimeout bounds how long ssh-proxy waits for a starting
// sandbox's supervisor relay (412 "sandbox is not ready").
const sessionReadyTimeout = 60 * time.Second

// ConnectOpts is `sandbox connect [name] [--editor vscode|cursor] [-- cmd]`.
type ConnectOpts struct {
	Name   string
	Editor string   // "", "vscode", "cursor"
	Argv   []string // optional remote command
}

// SSHProxyOpts mirrors OpenShell `ssh-proxy` flags.
type SSHProxyOpts struct {
	GatewayURL  string // --gateway (token mode) or --server (name / legacy mode)
	GatewayName string // --gateway-name / -g
	SandboxID   string // --sandbox-id (token mode)
	Token       string // --token (token mode)
	Name        string // --name or positional sandbox
}

// resolveGatewayURL picks an explicit URL, a named gateway, or the current one.
func (a *App) resolveGatewayURL(url, name string) (string, error) {
	if u := strings.TrimRight(strings.TrimSpace(url), "/"); u != "" {
		return u, nil
	}
	if name = strings.TrimSpace(name); name != "" {
		if looksLikeGatewayURL(name) {
			return strings.TrimRight(name, "/"), nil
		}
		cfg, _, err := gwconfig.Load()
		if err != nil {
			return "", err
		}
		g, ok := cfg.Gateways[name]
		if !ok || g.URL == "" {
			return "", fmt.Errorf("gateway %q not in config (cautem gateway add)", name)
		}
		return strings.TrimRight(g.URL, "/"), nil
	}
	return a.currentGatewayURL()
}

// proxyGatewayArgs identifies the active gateway for a ProxyCommand that may
// run without this shell's environment (IDEs): a config name when one maps to
// the URL, else the URL itself.
func (a *App) proxyGatewayArgs() ([]string, error) {
	u, err := a.currentGatewayURL()
	if err != nil {
		return nil, err
	}
	if cfg, _, err := gwconfig.Load(); err == nil {
		if a.GatewayNameOverride != "" {
			if g, ok := cfg.Gateways[a.GatewayNameOverride]; ok && strings.TrimRight(g.URL, "/") == u {
				return []string{"--gateway-name", a.GatewayNameOverride}, nil
			}
		}
		if g, ok := cfg.Gateways[cfg.Current]; ok && strings.TrimRight(g.URL, "/") == u {
			return []string{"--gateway-name", cfg.Current}, nil
		}
		for name, g := range cfg.Gateways {
			if strings.TrimRight(g.URL, "/") == u {
				return []string{"--gateway-name", name}, nil
			}
		}
	}
	return []string{"--server", u}, nil
}

// proxyCommandFor renders `<exe> ssh-proxy <gateway args> --name <sandbox>`.
func (a *App) proxyCommandFor(sandbox string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	gw, err := a.proxyGatewayArgs()
	if err != nil {
		return "", err
	}
	return sshconfig.ProxyCommand(exe, append(gw, "--name", sandbox)...), nil
}

// SandboxSSHHostBlock renders the managed Host block for a sandbox.
func (a *App) SandboxSSHHostBlock(name string) (string, error) {
	pc, err := a.proxyCommandFor(name)
	if err != nil {
		return "", err
	}
	return sshconfig.RenderHostBlock(sshconfig.Alias(name), pc), nil
}

// SandboxSSHConfig prints (and with install, installs) the OpenShell-style
// Host block for a sandbox.
func (a *App) SandboxSSHConfig(name string, install bool) error {
	block, err := a.SandboxSSHHostBlock(name)
	if err != nil {
		return err
	}
	if !install {
		fmt.Print(block)
		return nil
	}
	paths, err := sshconfig.DefaultPaths()
	if err != nil {
		return err
	}
	if err := sshconfig.Install(paths, sshconfig.Alias(name), block); err != nil {
		return err
	}
	fmt.Printf("ssh-config: Host %s → %s (included from %s)\n", sshconfig.Alias(name), paths.Managed, paths.User)
	return nil
}

// SandboxConnect is OpenShell `sandbox connect`: an SSH session through the
// gateway relay, or an IDE Remote-SSH window with --editor.
func (a *App) SandboxConnect(opt ConnectOpts) error {
	name := strings.TrimSpace(opt.Name)
	if name == "" {
		var err error
		name, err = a.resolveSandboxTarget("")
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
	}
	if ed := strings.ToLower(strings.TrimSpace(opt.Editor)); ed != "" {
		if err := a.openEditor(name, ed); err != nil {
			return err
		}
		a.rememberLastSandbox(name, "")
		return nil
	}
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("connect: OpenSSH client not found on PATH (install openssh-client)")
	}
	pc, err := a.proxyCommandFor(name)
	if err != nil {
		return err
	}
	tty := term.IsTerminal(int(os.Stdin.Fd()))
	cmd := exec.Command(sshBin, SSHCommandArgs(pc, sshconfig.Alias(name), tty, opt.Argv)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			a.rememberLastSandbox(name, "")
			return &ExitError{Code: ee.ExitCode()}
		}
		return err
	}
	a.rememberLastSandbox(name, "")
	return nil
}

// SSHCommandArgs builds the ssh(1) argv for connect (inline options so no
// ssh_config install is required).
func SSHCommandArgs(proxyCommand, alias string, tty bool, argv []string) []string {
	var args []string
	for _, kv := range sshconfig.Options() {
		args = append(args, "-o", kv[0]+"="+kv[1])
	}
	args = append(args, "-o", "ProxyCommand="+proxyCommand)
	if len(argv) > 0 {
		if tty {
			args = append(args, "-t")
		} else {
			args = append(args, "-T")
		}
	}
	args = append(args, alias)
	if len(argv) > 0 {
		quoted := make([]string, len(argv))
		for i, a := range argv {
			quoted[i] = posixQuote(a)
		}
		args = append(args, strings.Join(quoted, " "))
	}
	return args
}

func posixQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\"'\\$`;&|<>()*?![]{}#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// EditorCommand returns the launcher argv for Remote-SSH (OpenShell
// `--editor`): `cursor|code --remote ssh-remote+<alias> <workspace>`.
func EditorCommand(editor, alias string) ([]string, error) {
	var bin string
	switch editor {
	case "cursor":
		bin = "cursor"
	case "vscode", "code":
		bin = "code"
	default:
		return nil, fmt.Errorf("--editor must be vscode|cursor, got %q", editor)
	}
	return []string{bin, "--remote", "ssh-remote+" + alias, defaults.GuestWorkspace}, nil
}

func (a *App) openEditor(name, editor string) error {
	argv, err := EditorCommand(editor, sshconfig.Alias(name))
	if err != nil {
		return err
	}
	if err := a.SandboxSSHConfig(name, true); err != nil {
		return fmt.Errorf("editor: ssh-config: %w", err)
	}
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "editor: %q not on PATH; open %s manually: Remote-SSH → Connect to Host → %s\n",
			argv[0], editor, sshconfig.Alias(name))
		return nil
	}
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("editor: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	fmt.Printf("editor: %s → ssh-remote+%s %s\n", editor, sshconfig.Alias(name), defaults.GuestWorkspace)
	return nil
}

// SSHProxy is the ProxyCommand hook. stdout carries only SSH bytes.
func (a *App) SSHProxy(opt SSHProxyOpts) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	gwURL, err := a.resolveGatewayURL(opt.GatewayURL, opt.GatewayName)
	if err != nil {
		return err
	}
	sandbox, token := strings.TrimSpace(opt.SandboxID), strings.TrimSpace(opt.Token)
	revoke := func() {}
	if token == "" {
		name := strings.TrimSpace(opt.Name)
		if name == "" {
			return fmt.Errorf("ssh-proxy: --name <sandbox> (or --token with --sandbox-id) required")
		}
		c := a.clientFor(gwURL)
		sess, err := createSSHSessionWait(ctx, c, name, sessionReadyTimeout)
		if err != nil {
			return err
		}
		sandbox, token = sess.SandboxID, sess.Token
		if sandbox == "" {
			sandbox = name
		}
		revoke = func() {
			rctx, cancel := context.WithTimeout(context.Background(), TimeoutAPIShort)
			defer cancel()
			_ = c.RevokeSSHSession(rctx, sess.Token)
		}
	} else if sandbox == "" {
		sandbox = strings.TrimSpace(opt.Name)
	}
	if sandbox == "" {
		return fmt.Errorf("ssh-proxy: --sandbox-id required with --token")
	}
	defer revoke()
	conn, err := DialSSHRelay(ctx, gwURL, sandbox, token)
	if err != nil {
		return err
	}
	defer conn.Close()
	return bridgeStdio(ctx, conn, os.Stdin, os.Stdout)
}

// createSSHSessionWait retries while the supervisor relay comes up.
func createSSHSessionWait(ctx context.Context, c *cautem.Client, name string, timeout time.Duration) (cautem.SSHSession, error) {
	deadline := time.Now().Add(timeout)
	warned := false
	for {
		rctx, cancel := context.WithTimeout(ctx, TimeoutAPI)
		sess, err := c.CreateSSHSession(rctx, name)
		cancel()
		if err == nil {
			return sess, nil
		}
		if !errors.Is(err, cautem.ErrSandboxNotReady) || time.Now().After(deadline) {
			return cautem.SSHSession{}, fmt.Errorf("ssh-proxy: create ssh session for %q: %w", name, err)
		}
		if !warned {
			fmt.Fprintf(os.Stderr, "ssh-proxy: waiting for sandbox %s supervisor relay…\n", name)
			warned = true
		}
		select {
		case <-ctx.Done():
			return cautem.SSHSession{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// DialSSHRelay opens the gateway SSH tunnel (OpenShell ForwardTcp).
func DialSSHRelay(ctx context.Context, gwURL, sandbox, token string) (net.Conn, error) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	h.Set(relayproto.HeaderSandboxID, sandbox)
	conn, err := relayproto.Dial(ctx, gwURL, relayproto.PathSSHConnect, relayproto.DialOptions{Header: h})
	if err != nil {
		var se *relayproto.StatusError
		if errors.As(err, &se) && se.Code == http.StatusPreconditionFailed {
			return nil, fmt.Errorf("sandbox %q is not ready (supervisor relay not connected): %w", sandbox, err)
		}
		return nil, err
	}
	return conn, nil
}

func bridgeStdio(ctx context.Context, conn net.Conn, in io.Reader, out io.Writer) error {
	go func() {
		_, _ = io.Copy(conn, in)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_, err := io.Copy(out, conn)
	if err != nil && (errors.Is(err, net.ErrClosed) || ctx.Err() != nil) {
		return nil
	}
	return err
}

// openSSHClient is an in-process SSH client over the gateway relay; cleanup
// closes it and revokes the session.
func (a *App) openSSHClient(ctx context.Context, sandbox string) (*ssh.Client, func(), error) {
	gwURL, err := a.currentGatewayURL()
	if err != nil {
		return nil, nil, err
	}
	c := a.clientFor(gwURL)
	sess, err := createSSHSessionWait(ctx, c, sandbox, sessionReadyTimeout)
	if err != nil {
		return nil, nil, err
	}
	revoke := func() {
		rctx, cancel := context.WithTimeout(context.Background(), TimeoutAPIShort)
		defer cancel()
		_ = c.RevokeSSHSession(rctx, sess.Token)
	}
	conn, err := DialSSHRelay(ctx, gwURL, sess.SandboxID, sess.Token)
	if err != nil {
		revoke()
		return nil, nil, err
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, "sandbox", &ssh.ClientConfig{
		User: "sandbox",
		// Ephemeral host key; the relay is authenticated by the session token.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         TimeoutAPI,
	})
	if err != nil {
		_ = conn.Close()
		revoke()
		return nil, nil, fmt.Errorf("ssh handshake: %w", err)
	}
	client := ssh.NewClient(cc, chans, reqs)
	return client, func() { _ = client.Close(); revoke() }, nil
}
