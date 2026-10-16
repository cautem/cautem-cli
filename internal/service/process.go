package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cautem/cautem-core/defaults"
	display "github.com/cautem/cautem-display"
	"github.com/cautem/cautem-driver/driver"
	"github.com/cautem/cautem-runtime/sandbox"
	"github.com/cautem/cautem-sdk/go/cautem"
	"github.com/cautem/quarshell-cli/internal/ui"
	"github.com/cautem/slogx"
	"golang.org/x/term"
)

// ExecOpts for cautem sandbox exec.
type ExecOpts struct {
	Name         string
	Argv         []string
	TTY          bool
	Env          []string
	WorkDir      string
	Timeout      time.Duration // zero leaves execution bounded only by the caller context
	NoLoginShell bool
	Stdin        io.Reader
}

// Exec runs a command in a sandbox.

// Exec runs a command in a sandbox.
func (a *App) Exec(opt ExecOpts) error {
	if len(opt.Argv) == 0 {
		return fmt.Errorf("usage: cautem sandbox exec [name] [--workdir DIR] [--env K=V] -- CMD")
	}
	name, err := a.resolveSandboxTarget(opt.Name)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	opt.Name = name
	const op = "cli.sandbox.exec"
	log := a.op(op, slog.String("sandbox", opt.Name), slog.Int("argv_len", len(opt.Argv)))
	if opt.Name == "" || len(opt.Argv) == 0 {
		return fmt.Errorf("usage: cautem sandbox exec [name] [--workdir DIR] [--env K=V] -- CMD")
	}
	log.Info("executing in sandbox", slog.Bool("tty", opt.TTY), slog.Bool("no_login_shell", opt.NoLoginShell))
	commandCtx := a.CommandContext()
	routeCtx, routeCancel := context.WithTimeout(commandCtx, TimeoutAPI)
	client, remote, err := a.lifecycleGatewaySandbox(routeCtx, opt.Name)
	routeCancel()
	if err != nil {
		log.Error("failed to resolve sandbox runtime", slogx.Err(err))
		return fmt.Errorf("exec: %w", err)
	}
	if remote {
		execCtx := commandCtx
		if opt.Timeout > 0 {
			var cancel context.CancelFunc
			execCtx, cancel = context.WithTimeout(commandCtx, opt.Timeout+TimeoutExecGrace)
			defer cancel()
		}
		return a.execGateway(execCtx, client, opt, log)
	}
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		err := fmt.Errorf("exec: docker not available")
		log.Error("docker unavailable", slogx.Err(err))
		return err
	}
	// Always overlay credential placeholders from effective policy so attach/refresh
	// works without recreating the container (Docker Config.Env is immutable).
	guestEnv := a.credentialPlaceholdersForSandbox(opt.Name)
	// git ignores SSL_CERT_FILE; older sandboxes lack GIT_SSL_CAINFO at create-time.
	guestEnv = mergeEnvEntries(guestEnv, driver.CABundleEnv(defaults.GuestCAFile))
	guestEnv = mergeEnvEntries(guestEnv, opt.Env)
	// Match OpenShell's default login-shell startup for local sandboxes. The
	// driver receives a shell selector with argv encoded as quoted POSIX words.
	argv := localExecArgv(opt.Argv, opt.NoLoginShell)
	tty := opt.TTY
	ctx := commandCtx
	if opt.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(commandCtx, opt.Timeout)
		defer cancel()
	}
	a.emitProc(opt.Name, "LAUNCH", strings.Join(opt.Argv, " "), 0)
	if tty {
		if terminal := localExecTerminalExtension(a.Sandboxes.Driver); terminal != nil {
			code, terminalErr := runLocalExecTerminal(ctx, a.Sandboxes.Driver, terminal, opt, guestEnv)
			if terminalErr == nil {
				a.rememberLastSandbox(opt.Name, "")
			}
			a.emitProc(opt.Name, "EXIT", strings.Join(opt.Argv, " "), code)
			if terminalErr != nil {
				log.Error("exec failed", slogx.Err(terminalErr), slog.Int("exit_code", code))
				return terminalErr
			}
			if code != 0 {
				return &ExitError{Code: code}
			}
			return nil
		}
	}
	res, err := a.Sandboxes.Exec(ctx, opt.Name, driver.ExecRequest{
		Argv:        argv,
		TTY:         tty,
		Env:         guestEnv,
		WorkDir:     opt.WorkDir,
		StdinReader: opt.Stdin,
	})
	if outputErr := writeExecResult(res); outputErr != nil {
		return outputErr
	}
	code := 0
	if err == nil {
		a.rememberLastSandbox(opt.Name, "")
	}
	if res.ExitCode != 0 {
		code = res.ExitCode
	}
	a.emitProc(opt.Name, "EXIT", strings.Join(opt.Argv, " "), code)
	if err != nil {
		log.Error("exec failed", slogx.Err(err), slog.Int("exit_code", code))
		return err
	}
	if res.ExitCode != 0 {
		log.Info("exec finished with non-zero exit", slog.Int("exit_code", res.ExitCode))
		return &ExitError{Code: res.ExitCode}
	}
	log.Info("exec finished", slog.Int("exit_code", 0))
	return nil
}

func writeExecResult(result driver.ExecResult) error {
	if len(result.Stdout) > 0 {
		if _, err := os.Stdout.Write(result.Stdout); err != nil {
			return fmt.Errorf("write command stdout: %w", err)
		}
	}
	if len(result.Stderr) > 0 {
		if _, err := os.Stderr.Write(result.Stderr); err != nil {
			return fmt.Errorf("write command stderr: %w", err)
		}
	}
	if result.OutputTruncated {
		if _, err := fmt.Fprintln(os.Stderr, "cautem: command output was truncated by the compute driver"); err != nil {
			return fmt.Errorf("write command truncation notice: %w", err)
		}
	}
	return nil
}

func (a *App) execGateway(ctx context.Context, client *cautem.Client, opt ExecOpts, log *slogx.Logger) error {
	options := cautem.SandboxExecOptions{WorkDir: opt.WorkDir, NoLoginShell: opt.NoLoginShell, Env: execEnvMap(opt.Env)}
	a.emitProc(opt.Name, "LAUNCH", strings.Join(opt.Argv, " "), 0)
	var code int
	var err error
	if opt.TTY || opt.Stdin != nil {
		code, err = runGatewayInteractive(ctx, client, a.workspaceName(), opt, options)
	} else {
		code, err = runGatewayStream(ctx, client, a.workspaceName(), opt, options)
	}
	if err == nil {
		a.rememberLastSandbox(opt.Name, "")
	}
	a.emitProc(opt.Name, "EXIT", strings.Join(opt.Argv, " "), code)
	if err != nil {
		log.Error("gateway exec failed", slogx.Err(err), slog.Int("exit_code", code))
		return err
	}
	if code != 0 {
		log.Info("gateway exec finished with non-zero exit", slog.Int("exit_code", code))
		return &ExitError{Code: code}
	}
	log.Info("gateway exec finished", slog.Int("exit_code", code))
	return nil
}

func execEnvMap(entries []string) map[string]string {
	env := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

type gatewayExecClient interface {
	ExecStreamingWithTimeout(context.Context, string, string, []string, time.Duration, cautem.SandboxExecOptions) (cautem.SandboxExecStream, error)
	ExecInteractiveWithTTYTimeout(context.Context, string, string, []string, bool, uint32, uint32, time.Duration, cautem.SandboxExecOptions) (cautem.SandboxInteractiveOutput, error)
}

type resizableExecSession interface {
	Resize(cols, rows uint32) error
}

func runGatewayStream(ctx context.Context, client gatewayExecClient, workspace string, opt ExecOpts, options cautem.SandboxExecOptions) (int, error) {
	stream, err := client.ExecStreamingWithTimeout(ctx, workspace, opt.Name, opt.Argv, opt.Timeout, options)
	if err != nil {
		return -1, err
	}
	defer stream.Close()
	for {
		chunk, nextErr := stream.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return -1, nextErr
		}
		writer := io.Writer(os.Stdout)
		if chunk.Stream == "stderr" {
			writer = os.Stderr
		}
		if _, err := writer.Write(chunk.Data); err != nil {
			return -1, fmt.Errorf("write sandbox output: %w", err)
		}
	}
	return stream.ExitCode()
}

func runGatewayInteractive(ctx context.Context, client gatewayExecClient, workspace string, opt ExecOpts, options cautem.SandboxExecOptions) (int, error) {
	var cols, rows uint32
	if opt.TTY {
		if width, height, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
			cols, rows = uint32(width), uint32(height)
		}
	}
	session, err := client.ExecInteractiveWithTTYTimeout(ctx, workspace, opt.Name, opt.Argv, opt.TTY, cols, rows, opt.Timeout, options)
	if err != nil {
		return -1, err
	}
	defer session.Close()
	var restore func() error
	if term.IsTerminal(int(os.Stdin.Fd())) {
		state, rawErr := term.MakeRaw(int(os.Stdin.Fd()))
		if rawErr != nil {
			return -1, fmt.Errorf("enable terminal raw mode: %w", rawErr)
		}
		restore = func() error { return term.Restore(int(os.Stdin.Fd()), state) }
		defer restore()
	}
	if opt.TTY {
		stopResize := watchTerminalResize(ctx, session)
		defer stopResize()
	}
	inputDone := make(chan error, 1)
	go func() {
		if opt.Stdin == nil {
			inputDone <- cautem.CloseSandboxExecInput(session)
			return
		}
		_, copyErr := io.Copy(session, opt.Stdin)
		closeErr := cautem.CloseSandboxExecInput(session)
		inputDone <- errors.Join(copyErr, closeErr)
	}()
	outputErr := copyGatewayInteractiveOutput(session)
	code, exitErr := session.ExitCode()
	var inputErr error
	if term.IsTerminal(int(os.Stdin.Fd())) {
		select {
		case inputErr = <-inputDone:
		default:
			// A terminal reader cannot be interrupted portably after the remote command exits.
		}
	} else {
		inputErr = <-inputDone
	}
	return code, errors.Join(outputErr, inputErr, exitErr)
}

func copyGatewayInteractiveOutput(session cautem.SandboxInteractiveOutput) error {
	for {
		chunk, err := session.NextOutput()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		writer := io.Writer(os.Stdout)
		if chunk.Stream == "stderr" {
			writer = os.Stderr
		}
		if _, err := writer.Write(chunk.Data); err != nil {
			return fmt.Errorf("write sandbox output: %w", err)
		}
	}
}

func (a *App) emitProc(sandbox, activity, details string, exitCode int) {
	gw, err := a.currentGatewayURL()
	if err != nil || gw == "" || sandbox == "" {
		return
	}
	ts := time.Now().UTC()
	text := fmt.Sprintf("%s OCSF PROC:%s [INFO] ALLOWED %s", ts.Format(time.RFC3339Nano), activity, details)
	if activity == "EXIT" {
		text = fmt.Sprintf("%s OCSF PROC:EXIT [INFO] ALLOWED %s [exit:%d]", ts.Format(time.RFC3339Nano), details, exitCode)
	}
	c := cautem.NewWithToken(gw, a.gatewayTokenForURL(gw))
	ctx, cancel := a.withTimeout(TimeoutEmit)
	defer cancel()
	_ = c.PostLogs(ctx, sandbox, []cautem.LogLine{{
		TS: ts, Source: "proc", Level: "INFO", Text: text,
	}})
}

// RunOpts for cautem run (ensure sandbox + exec).

// RunOpts for cautem run (ensure sandbox + exec).
type RunOpts struct {
	Name        string
	Image       string
	Workspace   string
	Policy      string
	IKnow       bool
	Argv        []string
	NoTTY       bool
	NoProxy     bool
	NoHarden    bool
	Display     string
	DisplayPort int
	OpenDisplay bool
}

// Run ensures a sandbox exists, then execs the command (default: bash).

// Run ensures a sandbox exists, then execs the command (default: bash).
func (a *App) Run(opt RunOpts) error {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("run: docker not available")
	}
	ws := opt.Workspace
	if ws == "" {
		ws, _ = os.Getwd()
	}
	name := opt.Name
	if name == "" {
		name = filepath.Base(ws)
	}
	doc, policyPath, err := a.loadOrDenyAll(opt.Policy)
	if err != nil {
		return err
	}
	guestEnv := hostEnvForPolicy(doc)

	ctx, cancel := a.withTimeout(TimeoutWork)
	defer cancel()
	if _, err := a.Sandboxes.Driver.Inspect(ctx, name); err != nil {
		spec := driver.Spec{
			Name:          name,
			Image:         opt.Image,
			Workspace:     ws,
			IKnow:         opt.IKnow,
			Env:           guestEnv,
			PolicyPath:    policyPath,
			NoHarden:      opt.NoHarden,
			PersistVolume: true,
			ExtraHosts:    driver.HostGatewayExtraHosts(),
		}
		if display.ParseMode(opt.Display) == display.ModeNoVNC ||
			(doc.Display != nil && display.ParseMode(doc.Display.Mode) == display.ModeNoVNC) {
			pass, err := display.RandomPassword()
			if err != nil {
				return err
			}
			port := opt.DisplayPort
			if port <= 0 && doc.Display != nil && doc.Display.Port > 0 {
				port = doc.Display.Port
			}
			if port <= 0 {
				port = display.DefaultPort
			}
			spec.DisplayMode = "novnc"
			spec.DisplayPort = port
			spec.DisplayPassword = pass
			if spec.Image == "" {
				spec.Image = defaults.ImageGUI
			}
			url := display.NoVNCURL("127.0.0.1", port, pass)
			fmt.Fprintf(os.Stderr, "display: %s\n", url)
			fmt.Fprintf(os.Stderr, "display password: %s\n", pass)
			if opt.OpenDisplay {
				_ = display.OpenHostBrowser(url)
			}
		}
		if !opt.NoHarden {
			initBin, err := ensureInitBin(ctx)
			if err != nil {
				return err
			}
			spec.InitBin = initBin
		}
		if !opt.NoProxy {
			bin, err := ensureProxyBin(ctx)
			if err != nil {
				return err
			}
			spec.ProxyBin = bin
			if err := a.attachSupervisor(ctx, &spec, doc, name, ""); err != nil {
				return err
			}
		}
		h, err := a.Sandboxes.Create(ctx, sandbox.CreateOptions{
			Spec:   spec,
			Policy: doc,
		})
		if err != nil {
			return err
		}
		ui.Ok("sandbox %s created", h.Name)
		fmt.Fprintf(os.Stderr, "sandbox create: ok name=%s id=%s\n", h.Name, shortID(string(h.ID)))
	} else {
		info, _ := a.Sandboxes.Driver.Inspect(ctx, name)
		if info.Status != "" && info.Status != "running" {
			_ = a.Sandboxes.Driver.Start(ctx, info.ID)
		}
	}

	argv := opt.Argv
	if len(argv) == 0 {
		argv = ui.DefaultShellArgv()
	}
	tty := false
	if !opt.NoTTY {
		tty = term.IsTerminal(int(os.Stdin.Fd()))
	}
	return a.Exec(ExecOpts{
		Name: name,
		Argv: argv,
		TTY:  tty,
		Env:  guestEnv,
	})
}

// ProxyOpts for foreground host-side CONNECT proxy (debug / host-proxy mode).
