package service

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cautem/cautem-cli/internal/ui"
	"github.com/cautem/cautem-core/defaults"
	display "github.com/cautem/cautem-display"
	"github.com/cautem/cautem-driver/driver"
	"github.com/cautem/cautem-runtime/sandbox"
	"github.com/cautem/cautem-sdk/go/cautem"
	"github.com/cautem/slogx"
	"golang.org/x/term"
)

// ExecOpts for cautem sandbox exec.
type ExecOpts struct {
	Name    string
	Argv    []string
	TTY     bool
	Env     []string
	WorkDir string
}

// Exec runs a command in a sandbox.

// Exec runs a command in a sandbox.
func (a *App) Exec(opt ExecOpts) error {
	const op = "cli.sandbox.exec"
	log := a.op(op, slog.String("sandbox", opt.Name), slog.Int("argv_len", len(opt.Argv)))
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		err := fmt.Errorf("exec: docker not available")
		log.Error("docker unavailable", slogx.Err(err))
		return err
	}
	if opt.Name == "" || len(opt.Argv) == 0 {
		return fmt.Errorf("usage: cautem sandbox exec [--name] <name> [--workdir DIR] [--env K=V] -- CMD")
	}
	log.Info("executing in sandbox", slog.Bool("tty", opt.TTY))
	// Always overlay credential placeholders from effective policy so attach/refresh
	// works without recreating the container (Docker Config.Env is immutable).
	guestEnv := a.credentialPlaceholdersForSandbox(opt.Name)
	// git ignores SSL_CERT_FILE; older sandboxes lack GIT_SSL_CAINFO at create-time.
	guestEnv = mergeEnvEntries(guestEnv, driver.CABundleEnv(defaults.GuestCAFile))
	guestEnv = mergeEnvEntries(guestEnv, opt.Env)
	tty := opt.TTY
	// Interactive: honor Ctrl+C via command context; no artificial wall clock.
	ctx := a.CommandContext()
	a.emitProc(opt.Name, "LAUNCH", strings.Join(opt.Argv, " "), 0)
	res, err := a.Sandboxes.Exec(ctx, opt.Name, driver.ExecRequest{
		Argv:    opt.Argv,
		TTY:     tty,
		Env:     guestEnv,
		WorkDir: opt.WorkDir,
	})
	code := 0
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
