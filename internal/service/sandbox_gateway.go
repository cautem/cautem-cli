package service

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/cautem/cautem-sdk/go/cautem"
	"github.com/cautem/quarshell-cli/internal/storage/gwconfig"
	"golang.org/x/term"
)

// sandboxCreateRemote creates a Gateway-managed sandbox without provisioning a
// local Engine runtime. Remote options are checked before the API call because
// the current Control create contract only accepts an image or shared template,
// labels, and an optional canonical main command.
func (a *App) sandboxCreateRemote(opt SandboxCreateOpts) error {
	if err := validateTemplateCreateConflict(opt); err != nil {
		return err
	}
	if strings.TrimSpace(opt.Name) == "" {
		return fmt.Errorf("sandbox create --remote: --name is required")
	}
	if opt.BuildContext != "" || opt.Dockerfile != "" {
		return fmt.Errorf("sandbox create --remote: local source builds require a local compute driver")
	}
	if opt.WorkspaceFlagSet {
		return fmt.Errorf("sandbox create --remote: --workspace is a local bind mount; use --gateway-workspace")
	}
	if opt.Template != "" && (opt.CPU != 0 || opt.Memory != "" || opt.GPU || opt.GPUCount != 0 || len(opt.CDIDevices) > 0 || opt.Env != nil || opt.DriverConfigJSON != "") {
		return fmt.Errorf("sandbox create --remote: inline workload options cannot override a shared template")
	}
	if opt.Policy != "" || len(opt.Providers) > 0 || len(opt.Forwards) > 0 ||
		opt.Upload != "" || opt.NoGitIgnore || opt.Editor != "" || opt.Display != "" || opt.DisplayPort != 0 || opt.OpenDisplay ||
		opt.RestartPolicy != "" || opt.NoProxy || opt.NoHarden || opt.NoVolume || opt.NoKeep {
		return fmt.Errorf("sandbox create --remote: local policy, provider, upload, display, and runtime flags are not supported by the Gateway create contract; put workload fields in a shared template")
	}
	image := ""
	if opt.Template == "" {
		var err error
		image, err = gwconfig.ResolveImage(opt.From, opt.Image)
		if err != nil {
			return fmt.Errorf("sandbox create --remote source: %w", err)
		}
		if image == "" {
			return fmt.Errorf("sandbox create --remote: provide --image, --from, or a shared --template")
		}
		if opt.From != "" && strings.HasPrefix(image, "cautem-sandbox:") {
			return fmt.Errorf("sandbox create --remote: --from %q resolves to a local-only image; publish it, set CAUTEM_USE_GHCR=1, or pass a remotely pullable --image", opt.From)
		}
	}
	var cpuMillis uint32
	if math.IsNaN(opt.CPU) || math.IsInf(opt.CPU, 0) || opt.CPU < 0 {
		return fmt.Errorf("sandbox create --remote: CPU must be a finite non-negative quantity")
	}
	if opt.CPU > 0 {
		milli := opt.CPU * 1000
		if math.IsNaN(milli) || math.IsInf(milli, 0) || milli > 1_000_000 || math.Abs(milli-math.Round(milli)) > 1e-7 {
			return fmt.Errorf("sandbox create --remote: CPU must resolve to a whole number of millicores")
		}
		cpuMillis = uint32(math.Round(milli))
	}
	var memoryBytes uint64
	if opt.Memory != "" {
		parsed, parseErr := ParseMemoryQuantity(opt.Memory)
		if parseErr != nil || parsed <= 0 || parsed > 1<<50 {
			if parseErr == nil {
				parseErr = fmt.Errorf("must be positive and no greater than 1 PiB")
			}
			return fmt.Errorf("sandbox create --remote --memory: %w", parseErr)
		}
		memoryBytes = uint64(parsed)
	}
	if opt.GPUCount < 0 || opt.GPUCount > 1024 {
		return fmt.Errorf("sandbox create --remote: GPU count must be between 0 and 1024")
	}
	if err := validateSandboxCreateResources(opt); err != nil {
		return fmt.Errorf("sandbox create --remote: %w", err)
	}
	gpuCount := uint32(opt.GPUCount)
	if opt.GPU && gpuCount == 0 {
		gpuCount = 1
	}
	if raw := strings.TrimSpace(opt.DriverConfigJSON); raw != "" {
		var config map[string]any
		if err := json.Unmarshal([]byte(raw), &config); err != nil || config == nil {
			return fmt.Errorf("sandbox create --remote: --driver-config-json must be a JSON object keyed by compute driver")
		}
	}
	url, err := a.currentGatewayURL()
	if err != nil {
		return fmt.Errorf("sandbox create --remote: %w", err)
	}
	if url == "" {
		return fmt.Errorf("sandbox create --remote: no Gateway selected; use cautem gateway select")
	}
	workspace := strings.TrimSpace(opt.GatewayWorkspace)
	if workspace == "" {
		workspace = strings.TrimSpace(a.GlobalWorkspace)
	}
	if workspace == "" {
		workspace = "default"
	}
	client := a.clientFor(url)
	defer client.Close()
	client.Workspace = workspace
	ctx, cancel := a.withTimeout(TimeoutWork)
	defer cancel()
	created, err := client.CreateControlSandbox(ctx, cautem.Sandbox{
		Name: opt.Name, Workspace: workspace, Image: image,
		WorkloadTemplateName: opt.Template, Labels: opt.Labels,
		CPUMillis: cpuMillis, MemoryBytes: memoryBytes, GPUCount: gpuCount,
		Environment: opt.Env, DriverConfigJSON: opt.DriverConfigJSON,
		TTY:       opt.ForceTTY || (term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))),
		PidsLimit: opt.PidsLimit, CDIDevices: append([]string(nil), opt.CDIDevices...),
	}, canonicalMainCommand(opt.Argv))
	if err != nil {
		return err
	}
	a.rememberLastSandbox(created.Name, url)
	fmt.Printf("sandbox create: ok name=%s id=%s workspace=%s runtime=gateway image=%s\n",
		created.Name, shortID(created.ID), created.Workspace, created.Image)
	if len(opt.Argv) > 0 && !opt.Detach {
		if err := attachGatewayMainProcess(ctx, client, workspace, created.Name, opt.ForceTTY || (term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())))); err != nil {
			return fmt.Errorf("sandbox create: sandbox was created; attach failed, reconnect with `cautem sandbox attach %s`: %w", created.Name, err)
		}
	}
	return nil
}
