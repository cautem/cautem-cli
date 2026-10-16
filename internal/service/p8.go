package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"connectrpc.com/connect"
	"github.com/cautem/cautem-driver/driver"
	"github.com/cautem/cautem-sdk/go/cautem"
	"github.com/cautem/quarshell-cli/internal/storage/sandboxstate"
	"github.com/cautem/slogx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SandboxStop stops a sandbox without deleting network/volume.
func (a *App) SandboxStop(name string) error {
	var err error
	name, err = a.resolveSandboxTarget(name)
	if err != nil {
		return fmt.Errorf("sandbox stop: %w", err)
	}
	const op = "cli.sandbox.stop"
	log := a.op(op, slog.String("sandbox", name))
	log.Info("stopping sandbox")
	ctx, cancel := a.withTimeout(TimeoutWait)
	defer cancel()
	client, remote, err := a.lifecycleGatewaySandbox(ctx, name)
	if err != nil {
		return fmt.Errorf("sandbox stop: %w", err)
	}
	if remote {
		workspace := a.workspaceName()
		if _, err := client.StopControlSandbox(ctx, workspace, name); err != nil {
			log.Error("gateway failed to stop sandbox", slogx.Err(err))
			return fmt.Errorf("sandbox stop: %w", err)
		}
		a.rememberLastSandbox(name, "")
		log.Info("sandbox stopped by gateway")
		fmt.Printf("sandbox stop: ok %s\n", name)
		return nil
	}
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		err := fmt.Errorf("sandbox stop: docker not available")
		log.Error("docker unavailable", slogx.Err(err))
		return err
	}
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		log.Error("failed to inspect sandbox", slogx.Err(err))
		return err
	}
	if err := a.Sandboxes.Driver.Stop(ctx, info.ID); err != nil {
		log.Error("failed to stop sandbox", slogx.Err(err))
		return err
	}
	a.rememberLastSandbox(info.Name, "")
	a.touchGateway(ctx, info.Name, string(info.ID), info.Image, info.Network, "exited", nil)
	log.Info("sandbox stopped")
	fmt.Printf("sandbox stop: ok %s\n", name)
	return nil
}

// SandboxStart starts a previously stopped sandbox (volume retained).
func (a *App) SandboxStart(name string) error {
	var err error
	name, err = a.resolveSandboxTarget(name)
	if err != nil {
		return fmt.Errorf("sandbox start: %w", err)
	}
	const op = "cli.sandbox.start"
	log := a.op(op, slog.String("sandbox", name))
	log.Info("starting sandbox")
	ctx, cancel := a.withTimeout(TimeoutWait)
	defer cancel()
	client, remote, err := a.lifecycleGatewaySandbox(ctx, name)
	if err != nil {
		return fmt.Errorf("sandbox start: %w", err)
	}
	if remote {
		workspace := a.workspaceName()
		if _, err := client.StartControlSandbox(ctx, workspace, name); err != nil {
			log.Error("gateway failed to start sandbox", slogx.Err(err))
			return fmt.Errorf("sandbox start: %w", err)
		}
		a.rememberLastSandbox(name, "")
		log.Info("sandbox started by gateway")
		fmt.Printf("sandbox start: ok %s\n", name)
		return nil
	}
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		err := fmt.Errorf("sandbox start: docker not available")
		log.Error("docker unavailable", slogx.Err(err))
		return err
	}
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		log.Error("failed to inspect sandbox", slogx.Err(err))
		return err
	}
	if err := a.Sandboxes.Driver.Start(ctx, info.ID); err != nil {
		log.Error("failed to start sandbox", slogx.Err(err))
		return err
	}
	a.rememberLastSandbox(info.Name, "")
	a.touchGateway(ctx, info.Name, string(info.ID), info.Image, info.Network, "running", nil)
	log.Info("sandbox started")
	fmt.Printf("sandbox start: ok %s\n", name)
	return nil
}

// lifecycleGatewaySandbox distinguishes Gateway-managed compute from the
// CLI-managed runtime entries that must still be controlled by the local driver.
func (a *App) lifecycleGatewaySandbox(ctx context.Context, name string) (*cautem.Client, bool, error) {
	u, err := a.currentGatewayURL()
	if err != nil {
		if errors.Is(err, errNoCurrentGateway) {
			return nil, false, nil
		}
		return nil, false, err
	}
	client := a.clientFor(u)
	sandbox, err := client.GetControlSandbox(ctx, a.workspaceName(), name)
	if err != nil {
		if lifecycleLookupAllowsLocalFallback(err) {
			return client, false, nil
		}
		return nil, false, err
	}
	return client, !isCLIRuntimeSandbox(sandbox), nil
}

func lifecycleLookupAllowsLocalFallback(err error) bool {
	return connect.CodeOf(err) == connect.CodeNotFound || status.Code(err) == codes.NotFound
}

func isCLIRuntimeSandbox(sandbox cautem.Sandbox) bool {
	return sandbox.ComputeDriver == "cli-managed"
}

func (a *App) workspaceName() string {
	if a.GlobalWorkspace == "" {
		return "default"
	}
	return a.GlobalWorkspace
}

func (a *App) resolveSandboxTarget(name string) (string, error) {
	if name != "" {
		return name, nil
	}
	gateway := ""
	if url, err := a.currentGatewayURL(); err == nil {
		gateway = url
	} else {
		gateway = "local:" + a.DriverName
	}
	workspace := a.GlobalWorkspace
	if workspace == "" {
		workspace = "default"
	}
	name, err := sandboxstate.GetLast(gateway, workspace)
	if err != nil {
		return "", fmt.Errorf("load last sandbox: %w", err)
	}
	if name == "" {
		return "", fmt.Errorf("no sandbox name provided and no last-used sandbox; create or select a sandbox first")
	}
	return name, nil
}

func (a *App) rememberLastSandbox(name, gateway string) {
	if name == "" {
		return
	}
	if gateway == "" {
		if url, err := a.currentGatewayURL(); err == nil {
			gateway = url
		} else {
			gateway = "local:" + a.DriverName
		}
	}
	gateway = strings.TrimRight(gateway, "/")
	workspace := a.GlobalWorkspace
	if workspace == "" {
		workspace = "default"
	}
	if err := sandboxstate.SetLast(gateway, workspace, name); err != nil {
		slog.Warn("could not save last-used sandbox", "error", err)
	}
}

func (a *App) clearLastSandbox(name string) {
	if name == "" {
		return
	}
	gateway := ""
	if url, err := a.currentGatewayURL(); err == nil {
		gateway = strings.TrimRight(url, "/")
	} else {
		gateway = "local:" + a.DriverName
	}
	workspace := a.workspaceName()
	last, err := sandboxstate.GetLast(gateway, workspace)
	if err == nil && last == name {
		if err := sandboxstate.SetLast(gateway, workspace, ""); err != nil {
			slog.Warn("could not clear last-used sandbox", "error", err)
		}
	}
}

func (a *App) touchGateway(ctx context.Context, name, id, image, network, status string, labels map[string]string) {
	u, err := a.currentGatewayURL()
	if err != nil {
		return
	}
	if u == "" {
		return
	}
	_ = a.clientFor(u).UpsertSandbox(ctx, cautem.Sandbox{
		Name: name, ID: id, Image: image, Network: network, Status: status, Labels: labels,
	})
}

// Copy transfers files: host→sandbox or sandbox→host.
// Specs look like "localpath" and "name:/path" (sandbox side has a colon).
func (a *App) Copy(src, dst string) error {
	ctx, cancel := a.withTimeout(TimeoutWork)
	defer cancel()
	return a.copy(ctx, src, dst)
}

// SandboxUpload uploads a path with OpenShell-compatible .gitignore
// selection unless noGitIgnore is explicitly set.
func (a *App) SandboxUpload(src, dst string, noGitIgnore bool) error {
	ctx, cancel := a.withTimeout(TimeoutWork)
	defer cancel()
	sandboxName, _, sandboxTarget := splitSandboxPath(dst)
	if !sandboxTarget {
		return fmt.Errorf("sandbox upload: destination must be <sandbox>:/path")
	}
	client, remote, err := a.resolveTransferRoute(ctx, sandboxName)
	if err != nil {
		return fmt.Errorf("sandbox upload: %w", err)
	}
	if !remote && (a.Sandboxes == nil || a.Sandboxes.Driver == nil) {
		return fmt.Errorf("sandbox upload: docker not available")
	}
	staged, cleanup, filtered, err := prepareGitAwareUpload(ctx, src, !noGitIgnore)
	if err != nil {
		return err
	}
	defer cleanup()
	if filtered {
		fmt.Fprintf(os.Stderr, "sandbox upload: applying .gitignore rules to %s\n", src)
	} else if !noGitIgnore {
		fmt.Fprintf(os.Stderr, "sandbox upload: Git filtering was not applied to %s (outside a Git work tree or symlink source)\n", src)
	}
	if remote {
		_, remotePath, _ := splitSandboxPath(dst)
		return uploadGatewayPath(ctx, client, a.workspaceName(), sandboxName, staged, remotePath, TimeoutWork)
	}
	return a.copy(ctx, staged, dst)
}

func (a *App) copy(ctx context.Context, src, dst string) error {
	sName, sPath, sOK := splitSandboxPath(src)
	dName, dPath, dOK := splitSandboxPath(dst)
	if sOK == dOK {
		return fmt.Errorf("usage: cautem cp <local> <sandbox>:/path  OR  cautem cp <sandbox>:/path <local>")
	}
	sandboxName := sName
	if dOK {
		sandboxName = dName
	}
	client, remote, err := a.resolveTransferRoute(ctx, sandboxName)
	if err != nil {
		return err
	}
	if remote {
		if !sOK && dOK {
			return uploadGatewayPath(ctx, client, a.workspaceName(), sandboxName, src, dPath, TimeoutWork)
		}
		return downloadGatewayPath(ctx, client, a.workspaceName(), sandboxName, sPath, dst, TimeoutWork)
	}
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("sandbox transfer: docker not available")
	}
	switch {
	case !sOK && dOK:
		if err := driver.ValidateUploadDest(dPath); err != nil {
			return fmt.Errorf("cp: %w", err)
		}
		info, err := a.Sandboxes.Driver.Inspect(ctx, dName)
		if err != nil {
			return err
		}
		if err := a.Sandboxes.Driver.CopyTo(ctx, info.ID, src, dPath); err != nil {
			return err
		}
		fmt.Printf("cp: %s → %s:%s\n", src, dName, dPath)
	case sOK && !dOK:
		info, err := a.Sandboxes.Driver.Inspect(ctx, sName)
		if err != nil {
			return err
		}
		if err := a.Sandboxes.Driver.CopyFrom(ctx, info.ID, sPath, dst); err != nil {
			return err
		}
		fmt.Printf("cp: %s:%s → %s\n", sName, sPath, dst)
	}
	return nil
}

func (a *App) resolveTransferRoute(ctx context.Context, sandboxName string) (*cautem.Client, bool, error) {
	client, remote, err := a.lifecycleGatewaySandbox(ctx, sandboxName)
	if err != nil {
		return nil, false, fmt.Errorf("sandbox transfer: %w", err)
	}
	return client, remote, nil
}

func splitSandboxPath(s string) (name, path string, ok bool) {
	// Avoid treating Windows drive letters; we only support name:/abs
	i := strings.Index(s, ":/")
	if i <= 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}
