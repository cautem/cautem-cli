// Package service assembles the host-side CLI and runtime dependencies.
package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/cautem/cautem-core/engine"
	display "github.com/cautem/cautem-display"
	"github.com/cautem/cautem-driver/driver"
	_ "github.com/cautem/cautem-driver/driver/all"
	"github.com/cautem/cautem-proxy/proxy"
	"github.com/cautem/cautem-runtime/sandbox"
)

// managedSecretKeys tracks keys that must not fall back to host environment
// values after the gateway removes them.
type managedSecretKeys map[string]struct{}

// App holds constructed dependencies for CLI commands.

// App holds constructed dependencies for CLI commands.
type App struct {
	Sandboxes  *sandbox.Manager
	Display    display.Stack
	Docker     driver.Engine // Docker/Podman Engine API (nil for vm/k8s stubs)
	DriverName string        // "docker" | "podman" | "vm" | "kubernetes"

	// OpenShell global session (ApplyGlobal).
	OutputFormat        string
	GlobalWorkspace     string
	GatewayURLOverride  string
	GatewayNameOverride string

	// cmdCtx is the per-invocation context (SIGINT/SIGTERM from main).
	cmdCtx   context.Context
	secretMu sync.Mutex
	tokenMu  sync.Mutex
	// gatewaySecretKeys remembers keys that were gateway-managed so a later
	// empty snapshot cannot revive an older value from the process environment.
	gatewaySecretKeys map[string]managedSecretKeys
}

// New builds the default host-side graph.

// New builds the default host-side graph.
func New() *App {
	selected, selectionErr := selectedDriver()
	a := &App{Display: display.None{}, DriverName: selected}
	if selectionErr != nil {
		fmt.Fprintf(os.Stderr, "sandbox driver: %v\n", selectionErr)
		a.Sandboxes = &sandbox.Manager{}
		return a
	}
	switch a.DriverName {
	case "vm", "kubernetes":
		d, err := driver.Open(a.DriverName)
		if err != nil {
			a.Sandboxes = &sandbox.Manager{}
			return a
		}
		a.Sandboxes = &sandbox.Manager{Driver: d}
		return a
	}
	var eng driver.Engine
	var err error
	if a.DriverName == "podman" {
		var config map[string]any
		config, err = podmanDriverConfigFromEnvironment()
		if err == nil {
			eng, err = driver.OpenEngineWithConfig(a.DriverName, config)
		}
	} else {
		eng, err = driver.OpenEngine(a.DriverName)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "sandbox driver: %v\n", err)
		a.Sandboxes = &sandbox.Manager{}
		return a
	}
	if a.DriverName != "podman" {
		a.DriverName = "docker"
	}
	a.Docker = eng
	a.Sandboxes = &sandbox.Manager{
		Driver: eng,
		Proxy:  proxy.NewCONNECT(&engine.Allowlist{}),
	}
	return a
}

func selectedDriver() (string, error) {
	configured, err := configuredComputeDriversFromEnvironment()
	if err != nil {
		return "", err
	}
	selected := strings.ToLower(strings.TrimSpace(os.Getenv("CAUTEM_DRIVER")))
	if selected != "" {
		selected = normalizeSelectedDriver(selected)
		if selected != "docker" && selected != "podman" && selected != "vm" && selected != "kubernetes" {
			return "", fmt.Errorf("unsupported CAUTEM_DRIVER %q", selected)
		}
		if len(configured) > 0 && !slices.Contains(configured, selected) {
			return "", fmt.Errorf("CAUTEM_DRIVER %q is not selected by openshell.gateway.compute_drivers", selected)
		}
		return selected, nil
	}
	if len(configured) == 0 {
		return "docker", nil
	}
	if len(configured) == 1 {
		return configured[0], nil
	}
	if slices.Contains(configured, "docker") {
		return "docker", nil
	}
	return "", fmt.Errorf("multiple compute drivers are configured; set CAUTEM_DRIVER to one of: %s", strings.Join(configured, ", "))
}

func normalizeSelectedDriver(selected string) string {
	switch strings.ToLower(strings.TrimSpace(selected)) {
	case "podman":
		return "podman"
	case "vm", "microvm":
		return "vm"
	case "kubernetes", "k8s":
		return "kubernetes"
	default:
		return strings.ToLower(strings.TrimSpace(selected))
	}
}

func envTruthy(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// Banner is the short CLI intro.

// Version reports the CLI stub version.
// BuildVersion is set at release time:
// -ldflags "-X github.com/cautem/quarshell-cli/internal/service.BuildVersion=…".
var BuildVersion = "0.1.0-alpha.1"

func findModuleDir(modulePath string) (string, error) {
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	want := "module " + modulePath
	short := strings.TrimPrefix(modulePath, "github.com/cautem/")
	for _, start := range candidates {
		dir := start
		for range 8 {
			gm := filepath.Join(dir, "go.mod")
			b, err := os.ReadFile(gm)
			if err == nil && strings.Contains(string(b), want) {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		try := filepath.Join(start, short)
		if b, err := os.ReadFile(filepath.Join(try, "go.mod")); err == nil &&
			strings.Contains(string(b), want) {
			return try, nil
		}
	}
	return "", fmt.Errorf("cannot find module %s source", modulePath)
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
