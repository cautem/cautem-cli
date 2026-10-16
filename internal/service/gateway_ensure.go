package service

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cautem/quarshell-core/defaults"
	"github.com/cautem/quarshell-slogx"
)

const localGatewayName = "local"
const localGatewayURL = "http://" + defaults.GatewayListen

// GatewayEnsure makes sure a reachable gateway is selected (OpenShell install UX).
// If the current gateway is healthy, it is a no-op. Otherwise it starts a local
// cautem-gateway on 127.0.0.1:7443 (sibling binary or PATH), registers it as "local",
// and selects it.
func (a *App) GatewayEnsure() error {
	const op = "cli.gateway.ensure"
	log := a.op(op)
	if u, err := a.currentGatewayURL(); err == nil && u != "" {
		ctx, cancel := a.withTimeout(TimeoutProbe)
		defer cancel()
		if _, err := a.clientFor(u).Healthz(ctx); err == nil {
			log.Info("gateway already healthy", slog.String("url", u))
			return nil
		}
	}
	// Prefer already-listening local port (user started gateway manually).
	if portOpen("127.0.0.1", defaults.GatewayPort) {
		_ = a.GatewayAdd(localGatewayName, localGatewayURL)
		_ = a.GatewaySelect(localGatewayName)
		ctx, cancel := a.withTimeout(TimeoutProbe)
		defer cancel()
		if _, err := a.clientFor(localGatewayURL).Healthz(ctx); err == nil {
			log.Info("using existing local gateway", slog.String("url", localGatewayURL))
			fmt.Printf("gateway ensure: using existing %s\n", localGatewayURL)
			return nil
		}
	}
	bin, err := findGatewayBinary()
	if err != nil {
		log.Error("gateway binary not found", slogx.Err(err))
		return fmt.Errorf("gateway ensure: %w\nStart manually: cautem-gateway --listen %s\nThen: cautem gateway add local --url %s && cautem gateway select local",
			err, defaults.GatewayListen, localGatewayURL)
	}
	logPath, err := gatewayLogPath()
	if err != nil {
		log.Error("failed to resolve gateway log path", slogx.Err(err))
		return err
	}
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.Error("failed to open gateway log", slogx.Err(err))
		return fmt.Errorf("gateway ensure: log: %w", err)
	}
	log.Info("starting local gateway", slog.String("bin", bin), slog.String("listen", defaults.GatewayListen))
	cmd := exec.Command(bin, "--listen", defaults.GatewayListen, "--data-dir", defaultGatewayDataDir())
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = gatewaySysProcAttr()
	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		log.Error("failed to start gateway", slogx.Err(err))
		return fmt.Errorf("gateway ensure: start %s: %w", bin, err)
	}
	// Detach: do not wait; log file stays open in child.
	go func() {
		_ = cmd.Wait()
		_ = logF.Close()
	}()
	deadline := time.Now().Add(gatewayStartupTimeout)
	for time.Now().Before(deadline) {
		ctx, cancel := a.withTimeout(TimeoutProbeFast)
		_, err := a.clientFor(localGatewayURL).Healthz(ctx)
		cancel()
		if err == nil {
			_ = a.GatewayAdd(localGatewayName, localGatewayURL)
			_ = a.GatewaySelect(localGatewayName)
			log.Info("local gateway ready", slog.Int("pid", cmd.Process.Pid), slog.String("log", logPath))
			fmt.Printf("gateway ensure: started %s (pid %d, log %s)\n", localGatewayURL, cmd.Process.Pid, logPath)
			return nil
		}
		time.Sleep(gatewayStartupPollInterval)
	}
	err = fmt.Errorf("gateway ensure: started %s but healthz not ready (see %s)", bin, logPath)
	log.Error("gateway healthz timeout", slogx.Err(err))
	return err
}

func portOpen(host string, port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), TimeoutDial)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func findGatewayBinary() (string, error) {
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "cautem-gateway"),
			filepath.Join(dir, "cautem-gateway-darwin-arm64"),
			filepath.Join(dir, "cautem-gateway-darwin-amd64"),
			filepath.Join(dir, "cautem-gateway-linux-arm64"),
			filepath.Join(dir, "cautem-gateway-linux-amd64"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "cautem-gateway"),
			filepath.Join(wd, "cautem-cli", "cautem-gateway"),
			filepath.Join(wd, "cautem-gateway", "cautem-gateway"),
		)
	}
	if p, err := exec.LookPath("cautem-gateway"); err == nil {
		candidates = append(candidates, p)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode().Perm()&0o111 != 0 {
			return c, nil
		}
	}
	return "", fmt.Errorf("cautem-gateway binary not found next to cautem or on PATH")
}

func gatewayLogPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "cautem")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "gateway.log"), nil
}

// InferProvidersFromArgv maps trailing create commands to provider profile ids
// (OpenShell detect_provider_from_command).
func InferProvidersFromArgv(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	base := strings.ToLower(filepath.Base(argv[0]))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "agent", "cursor-agent", "cursor":
		return []string{"cursor"}
	case "claude", "claude-code":
		return []string{"claude-code"}
	case "codex":
		return []string{"codex"}
	case "gh", "git":
		return []string{"github"}
	default:
		return nil
	}
}
