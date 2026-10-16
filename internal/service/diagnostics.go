package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/cautem/cautem-core/defaults"
	"github.com/cautem/cautem-driver/driver"
	"github.com/cautem/cautem-runtime/secrets"
	"github.com/cautem/quarshell-cli/internal/outfmt"
)

// Banner is the short CLI intro.
func (a *App) Banner() string {
	return "cautem — agent sandbox CLI"
}

// Version reports the CLI stub version.
// BuildVersion is set at release time:
// -ldflags "-X github.com/cautem/quarshell-cli/internal/service.BuildVersion=…".

func (a *App) Version() string { return "cautem " + BuildVersion }

// Health probes Docker Engine / Podman API.

// Health probes Docker Engine / Podman API.
func (a *App) Health() error {
	if a.Docker == nil {
		hint := "check DOCKER_HOST / Docker Desktop"
		switch a.DriverName {
		case "podman":
			hint = "check CAUTEM_PODMAN_SOCKET / podman.socket (systemctl --user start podman.socket)"
		case "vm":
			return fmt.Errorf("health: vm driver is a spike stub (see docs/exp/MICROVM.md)")
		case "kubernetes":
			return fmt.Errorf("health: kubernetes driver is a spike stub (see docs/exp/KUBERNETES.md)")
		}
		return fmt.Errorf("health: %s client unavailable (%s)", a.DriverName, hint)
	}
	ctx, cancel := a.withTimeout(TimeoutAPI)
	defer cancel()
	p := a.Docker.Health(ctx)
	if !p.OK {
		return fmt.Errorf("health: %s unreachable: %s", a.DriverName, p.Error)
	}
	engine := strings.ToLower(strings.TrimSpace(a.DriverName))
	if engine == "" {
		engine = "compute"
	}
	fmt.Printf("health: ok\n")
	fmt.Printf("  driver:           %s\n", a.DriverName)
	fmt.Printf("  %s.server:  %s\n", engine, p.ServerVersion)
	fmt.Printf("  %s.api:     %s\n", engine, p.APIVersion)
	fmt.Printf("  %s.os:      %s\n", engine, p.OperatingSystem)
	fmt.Printf("  %s.arch:    %s\n", engine, p.Architecture)
	fmt.Printf("  %s.context: %s\n", engine, p.Context)
	fmt.Printf("  isolation:        %s\n", p.Isolation)
	fmt.Printf("  host.goos:        %s\n", p.HostGOOS)
	fmt.Printf("  capabilities:     %s\n", strings.Join(p.Capabilities, ", "))
	if strings.EqualFold(a.DriverName, "podman") {
		fmt.Printf("  podman.rootless:  %s\n", p.Rootless)
		fmt.Printf("  podman.security:  %s\n", strings.Join(p.SecurityOptions, ", "))
	}
	probeCtx, probeCancel := a.withTimeout(TimeoutWait)
	defer probeCancel()
	probe := a.probeLandlock(probeCtx)
	fmt.Printf("  landlock:         %s\n", probe)
	fmt.Printf("  seccomp:          %s\n", driver.SeccompNote())
	for _, img := range []string{defaults.ImageLocal, defaults.ImageCursor} {
		ok := a.Docker.ImagePresent(ctx, img)
		state := "missing (pulled from GHCR on first use)"
		if strings.EqualFold(os.Getenv(defaults.EnvImagePull), "never") {
			state = "missing (task runtime:image:cli / task docker:agent:cursor)"
		}
		if ok {
			state = "present"
		}
		fmt.Printf("  image %-18s %s\n", img+":", state)
	}
	fmt.Printf("  linux helpers:    %s\n", helperStatus())
	if u, err := a.currentGatewayURL(); err == nil && u != "" {
		gctx, gcancel := a.withTimeout(TimeoutAPIShort)
		defer gcancel()
		cli := a.clientFor(u)
		if _, err := cli.Healthz(gctx); err != nil {
			fmt.Printf("  gateway:          unreachable (%s)\n", u)
		} else {
			fmt.Printf("  gateway:          ok (%s)\n", u)
			if info, err := cli.Info(gctx); err == nil {
				printSecretsKEK(info)
			}
		}
	} else {
		fmt.Printf("  gateway:          not selected (cautem gateway ensure)\n")
	}
	if envTruthy("CAUTEM_LANDLOCK_REQUIRED") && !landlockABIAtLeast(probe, 1) {
		return fmt.Errorf("health: landlock gate failed (need ABI≥1; got %s). Unset CAUTEM_LANDLOCK_REQUIRED on Docker Desktop / ABI 0 hosts", probe)
	}
	return nil
}

func printSecretsKEK(info map[string]any) {
	raw, ok := info["secrets_kek"].(map[string]any)
	if !ok {
		return
	}
	src, _ := raw["source"].(string)
	pinned, _ := raw["pinned"].(bool)
	format, _ := raw["format"].(string)
	fmt.Printf("  secrets_kek:      source=%s pinned=%v format=%s\n", src, pinned, format)
	if w, _ := raw["warning"].(string); strings.TrimSpace(w) != "" {
		fmt.Printf("  secrets_kek.warn: %s\n", w)
	}
}

// Doctor is the production Docker-path readiness check (OpenShell doctor alias).

// Doctor is the production Docker-path readiness check (OpenShell doctor alias).
func (a *App) Doctor() error {
	if err := a.Health(); err != nil {
		return err
	}
	probeCtx, probeCancel := a.withTimeout(TimeoutAPIShort)
	probe := a.Docker.Health(probeCtx)
	probeCancel()
	if err := validateDoctorEngine(a.DriverName, probe); err != nil {
		return err
	}
	u, err := a.currentGatewayURL()
	if err != nil || u == "" {
		return fmt.Errorf("doctor: no gateway selected (cautem gateway ensure|add|select)")
	}
	ctx, cancel := a.withTimeout(TimeoutAPIShort)
	defer cancel()
	cli := a.clientFor(u)
	info, err := cli.Info(ctx)
	if err != nil {
		return fmt.Errorf("doctor: gateway info: %w", err)
	}
	engine := strings.ToLower(strings.TrimSpace(a.DriverName))
	if engine == "" {
		engine = "compute"
	}
	if raw, ok := info["secrets_kek"].(map[string]any); ok {
		if pinned, _ := raw["pinned"].(bool); !pinned {
			fmt.Fprintf(os.Stderr, "doctor: warn: pin %s in compose/env so secrets survive volume loss\n", secrets.EnvKEK)
		}
		if needed, _ := raw["migration_needed"].(bool); needed {
			fmt.Fprintln(os.Stderr, "doctor: warn: legacy secrets store needs migration; back up the data dir and restart gateway")
		}
	}
	if strings.EqualFold(a.DriverName, "docker") {
		fmt.Fprintln(os.Stderr, "doctor: note: binary-scoped egress rules deny callers whose executable cannot be verified; Docker Desktop sidecar TCP usually cannot provide this identity")
	}
	providers, err := cli.ListProviders(ctx)
	if err != nil {
		return fmt.Errorf("doctor: list providers: %w", err)
	}
	fmt.Printf("  providers:        %d instance(s)\n", len(providers))
	for _, p := range providers {
		fmt.Printf("    - %s type=%s env=%v\n", p.Name, p.Type, p.EnvVars)
	}
	fmt.Printf("doctor: ok (%s path)\n", engine)
	return nil
}

func validateDoctorEngine(name string, probe driver.Probe) error {
	if !probe.OK {
		return fmt.Errorf("doctor: %s engine is unavailable: %s", name, probe.Error)
	}
	if !strings.EqualFold(strings.TrimSpace(name), "podman") {
		return nil
	}
	major, ok := leadingVersionNumber(probe.ServerVersion)
	if !ok {
		return fmt.Errorf("doctor: Podman server version %q is not recognizable; run `podman version` and verify the active API socket", probe.ServerVersion)
	}
	if major < 6 {
		return fmt.Errorf("doctor: Podman %s cannot enforce the host-gateway blocking route required by proxy-backed sandboxes; upgrade the selected Podman service to 6 or newer", probe.ServerVersion)
	}
	if !slices.Contains(probe.Capabilities, "libpod-native") || !slices.Contains(probe.Capabilities, "userns") {
		return fmt.Errorf("doctor: Podman API lacks native network/userns capabilities; select the Podman 6 Libpod socket and retry")
	}
	if strings.TrimSpace(probe.Rootless) == "" {
		return fmt.Errorf("doctor: Podman rootless mode could not be determined; check `podman info` and the selected user/system socket")
	}
	return nil
}

func leadingVersionNumber(version string) (int, bool) {
	version = strings.TrimSpace(version)
	start := -1
	for index, char := range version {
		if char >= '0' && char <= '9' {
			start = index
			break
		}
	}
	if start < 0 {
		return 0, false
	}
	major := 0
	for _, char := range version[start:] {
		if char < '0' || char > '9' {
			break
		}
		major = major*10 + int(char-'0')
	}
	return major, major > 0
}

// CleanupDockerTestResources removes only disposable resources created by the
// Testcontainers/cautem test lanes. It deliberately does not use
// `docker system prune`: named volumes, running containers and user networks
// are outside this command's scope.

// CleanupDockerTestResources removes only disposable resources created by the
// Testcontainers/cautem test lanes. It deliberately does not use
// `docker system prune`: named volumes, running containers and user networks
// are outside this command's scope.
type CleanupReport struct {
	AnonymousVolumes  int  `json:"anonymous_volumes" yaml:"anonymous_volumes"`
	StoppedContainers int  `json:"stopped_containers" yaml:"stopped_containers"`
	DryRun            bool `json:"dry_run" yaml:"dry_run"`
	Removed           bool `json:"removed" yaml:"removed"`
}

func (a *App) CleanupDockerTestResources(ctx context.Context, confirm bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	list := func(args ...string) ([]string, error) {
		out, err := exec.CommandContext(ctx, "docker", args...).Output()
		if err != nil {
			return nil, fmt.Errorf("doctor cleanup: docker %s: %w", strings.Join(args, " "), err)
		}
		var values []string
		for _, line := range strings.Split(string(out), "\n") {
			if value := strings.TrimSpace(line); value != "" {
				values = append(values, value)
			}
		}
		return values, nil
	}
	volumes, err := list("volume", "ls", "-q", "--filter", "label=com.docker.volume.anonymous", "--filter", "dangling=true")
	if err != nil {
		return err
	}
	containers, err := list("ps", "-aq", "--filter", "label=cautem=1", "--filter", "status=exited")
	if err != nil {
		return err
	}
	report := CleanupReport{AnonymousVolumes: len(volumes), StoppedContainers: len(containers), DryRun: !confirm, Removed: confirm}
	if !confirm {
		return a.emitCleanupReport(report)
	}
	for _, volume := range volumes {
		if out, err := exec.CommandContext(ctx, "docker", "volume", "rm", volume).CombinedOutput(); err != nil {
			return fmt.Errorf("doctor cleanup: remove volume %s: %w (%s)", volume, err, strings.TrimSpace(string(out)))
		}
	}
	for _, container := range containers {
		if out, err := exec.CommandContext(ctx, "docker", "rm", "-f", container).CombinedOutput(); err != nil {
			return fmt.Errorf("doctor cleanup: remove container %s: %w (%s)", container, err, strings.TrimSpace(string(out)))
		}
	}
	return a.emitCleanupReport(report)
}

func (a *App) emitCleanupReport(report CleanupReport) error {
	format := "text"
	if a != nil && a.OutputFormat != "" {
		format = a.OutputFormat
	}
	return outfmt.Emit(os.Stdout, format, func(w io.Writer) error {
		if report.DryRun {
			_, _ = fmt.Fprintf(w, "doctor cleanup: %d anonymous test volume(s), %d stopped test container(s)\n", report.AnonymousVolumes, report.StoppedContainers)
			_, _ = fmt.Fprintln(w, "doctor cleanup: dry-run; repeat with --yes to remove these resources")
			return nil
		}
		_, err := fmt.Fprintln(w, "doctor cleanup: removed scoped test resources")
		return err
	}, report)
}

func landlockABIAtLeast(probe string, min int) bool {
	// probe examples: "abi=2 (probe ok)", "abi=0 (...)"
	const p = "abi="
	i := strings.Index(strings.ToLower(probe), p)
	if i < 0 {
		return false
	}
	rest := probe[i+len(p):]
	n := 0
	for _, c := range rest {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n >= min
}

func (a *App) probeLandlock(ctx context.Context) string {
	initBin, err := ensureInitBin(ctx)
	if err != nil {
		return "probe skipped (" + err.Error() + ")"
	}
	if a.Docker == nil {
		return "docker unavailable"
	}
	out, err := a.Docker.RunProbe(ctx, initBin)
	if err != nil {
		return "probe error: " + err.Error()
	}
	return out
}

// PolicyCheck loads and validates a policy YAML file.
