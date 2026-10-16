package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/cautem/cautem-cli/internal/autoprovider"
	"github.com/cautem/cautem-cli/internal/storage/gwconfig"
	"github.com/cautem/cautem-cli/internal/storage/templates"
	"github.com/cautem/cautem-cli/internal/ui"
	core "github.com/cautem/cautem-core"
	"github.com/cautem/cautem-core/defaults"
	"github.com/cautem/cautem-core/env"
	display "github.com/cautem/cautem-display"
	"github.com/cautem/cautem-driver/driver"
	"github.com/cautem/cautem-runtime/sandbox"
	"github.com/cautem/cautem-sdk/go/cautem"
	"github.com/cautem/slogx"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// SandboxCreateOpts are CLI flags for sandbox create.
type SandboxCreateOpts struct {
	Name           string
	Image          string
	Workspace      string
	BuildContext   string // local Docker-compatible build context
	Dockerfile     string // path relative to BuildContext; defaults to Dockerfile
	Policy         string
	IKnow          bool
	NoProxy        bool
	NoHarden       bool
	Display        string // none | novnc
	DisplayPort    int
	OpenDisplay    bool
	Labels         map[string]string
	NoHostInternal bool // skip host.cautem.internal ExtraHosts
	GatewayURL     string
	From           string // BYOC / community image alias
	NoVolume       bool   // skip persist GuestData volume (default: persist)
	GPU            bool
	GPUCount       int
	CDIDevices     []string
	Argv           []string // after `--`: canonical main command
	// Providers are profile ids or existing instance names (OpenShell --provider).
	// On create: discover host env, ensure gateway instance, compose policy before start, attach.
	Providers []string
	// AutoProviders enables OpenShell-style inference from trailing argv.
	AutoProviders bool
	// NoAutoProviders forces ModeOff even if AutoProviders is set.
	NoAutoProviders bool
	// Env is non-secret create-time environment (KEY=VALUE). Credential-looking keys warn.
	Env map[string]string
	// NoCredentialWarnings suppresses --env credential-looking key warnings.
	NoCredentialWarnings bool

	Detach           bool
	Editor           string // vscode | cursor
	Forwards         []int
	Upload           string
	NoGitIgnore      bool
	CPU              float64
	Memory           string
	PidsLimit        int64 // 0 unset (driver default); -1 unlimited; >0 explicit
	Template         string
	RestartPolicy    string // never | on-failure | always
	Remote           bool   // create through the selected Gateway's compute backend
	GatewayWorkspace string
	WorkspaceFlagSet bool

	DriverConfigJSON string // --driver-config-json
	ApprovalMode     string // manual|auto
	NoKeep           bool   // delete sandbox after main command exits
	ForceTTY         bool   // --tty force a PTY for the canonical main process

}

// SandboxCreate creates and starts a sandbox.

// SandboxCreate creates and starts a sandbox.
func (a *App) SandboxCreate(opt SandboxCreateOpts) error {
	const op = "cli.sandbox.create"
	log := a.op(op)
	if opt.Remote {
		return a.sandboxCreateRemote(opt)
	}
	if err := validateCreateSource(opt); err != nil {
		return err
	}
	if err := validateTemplateCreateConflict(opt); err != nil {
		return err
	}
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		err := fmt.Errorf("sandbox create: docker not available")
		log.Error("docker unavailable", slogx.Err(err))
		return err
	}
	if opt.Template != "" {
		tpl, err := templates.Get(opt.Template)
		if err != nil {
			log.Error("failed to load template", slogx.Err(err), slog.String("template", opt.Template))
			return fmt.Errorf("sandbox create template: %w", err)
		}
		mergeTemplateIntoCreate(&opt, tpl)
	}
	applyCreateDefaults(&opt)
	if err := validateSandboxCreateResources(opt); err != nil {
		return err
	}
	var memoryBytes int64
	if opt.Memory != "" {
		parsedMemory, parseErr := ParseMemoryQuantity(opt.Memory)
		if parseErr != nil {
			return fmt.Errorf("sandbox create --memory: %w", parseErr)
		}
		memoryBytes = parsedMemory
	}
	var err error
	if strings.TrimSpace(opt.BuildContext) == "" {
		resolvedImage, resolveErr := gwconfig.ResolveImage(opt.From, opt.Image)
		if resolveErr != nil {
			return fmt.Errorf("sandbox create source: %w", resolveErr)
		}
		opt.Image = resolvedImage
	}
	if opt.NoGitIgnore && opt.Upload == "" {
		return fmt.Errorf("sandbox create: --no-git-ignore requires --upload")
	}
	ctx, cancel := a.withTimeout(TimeoutWork)
	defer cancel()
	preparedUpload := opt.Upload
	uploadFiltered := false
	uploadDest := ""
	if opt.Upload != "" {
		stagedUpload, cleanupUpload, filteredUpload, preflightErr := prepareGitAwareUpload(ctx, opt.Upload, !opt.NoGitIgnore)
		if preflightErr != nil {
			return fmt.Errorf("sandbox create --upload preflight: %w", preflightErr)
		}
		defer cleanupUpload()
		preparedUpload, uploadFiltered = stagedUpload, filteredUpload
		uploadDest, err = sandboxCreateUploadDestination(preparedUpload, opt.Upload)
		if err != nil {
			return fmt.Errorf("sandbox create --upload preflight: %w", err)
		}
		if err := driver.ValidateUploadDest(uploadDest); err != nil {
			return fmt.Errorf("sandbox create --upload: %w", err)
		}
		if uploadFiltered {
			fmt.Fprintf(os.Stderr, "sandbox create --upload: applying .gitignore rules to %s\n", opt.Upload)
		} else if !opt.NoGitIgnore {
			fmt.Fprintf(os.Stderr, "sandbox create --upload: Git filtering was not applied to %s (outside a Git work tree or symlink source)\n", opt.Upload)
		}
	}
	ws := opt.Workspace
	if ws == "" && a.GlobalWorkspace != "" && a.GlobalWorkspace != "default" {
		ws = a.GlobalWorkspace
	}
	if ws == "" {
		ws, _ = os.Getwd()
	}
	name := opt.Name
	if name == "" {
		name = filepath.Base(ws)
	}
	if strings.TrimSpace(opt.BuildContext) != "" {
		image, buildErr := buildLocalSourceImage(ctx, a.Sandboxes.Driver, opt.BuildContext, opt.Dockerfile, name, os.Stderr)
		if buildErr != nil {
			return fmt.Errorf("sandbox create source build: %w", buildErr)
		}
		opt.Image = image
	}
	createTTY := opt.ForceTTY || (term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())))
	canonicalArgv := canonicalMainCommand(opt.Argv)
	log = log.With(slog.String("sandbox", name), slog.String("workspace", ws))
	log.Info("creating sandbox")
	baseDoc, basePath, err := a.loadOrDenyAll(opt.Policy)
	if err != nil {
		log.Error("failed to load policy", slogx.Err(err))
		return err
	}
	gwURL := opt.GatewayURL
	if gwURL == "" {
		// Production Docker path: gateway is required for secrets + registry unless --no-proxy
		// and no providers (dev escape hatch).
		needGW := !opt.NoProxy || len(opt.Providers) > 0 || opt.AutoProviders
		if err := a.GatewayEnsure(); err != nil {
			if needGW {
				return fmt.Errorf("sandbox create: %w", err)
			}
			fmt.Fprintf(os.Stderr, "sandbox create: warn: %v\n", err)
		}
		if cfg, _, err := gwconfig.Load(); err == nil {
			gwURL = gwconfig.CurrentURL(cfg)
		}
	}
	if gwURL == "" && (!opt.NoProxy || len(opt.Providers) > 0) {
		return fmt.Errorf("sandbox create: gateway required for proxy/providers (cautem gateway ensure)")
	}
	if !opt.NoCredentialWarnings {
		hints := map[string][]env.ProfileHint{}
		for k := range opt.Env {
			if hs := env.HintKey(k); len(hs) > 0 {
				hints[k] = hs
			}
		}
		env.WarnCredentialEnv(os.Stderr, opt.Env, hints, false)
	}
	mode := autoprovider.ModeOff
	switch {
	case opt.NoAutoProviders:
		mode = autoprovider.ModeOff
	case opt.AutoProviders:
		mode = autoprovider.ModeOn
	default:
		// Known trailing agents auto-create providers unless --no-auto-providers is set.
		if len(InferProvidersFromArgv(opt.Argv)) > 0 {
			mode = autoprovider.ModeOn
		}
	}
	opt.Providers = autoprovider.Merge(opt.Providers, InferProvidersFromArgv(opt.Argv), mode)
	attached, doc, policyPath, err := a.prepareProviders(baseDoc, basePath, opt.Providers, gwURL)
	if err != nil {
		return err
	}
	if opt.Labels == nil {
		opt.Labels = map[string]string{}
	}
	if am := strings.ToLower(strings.TrimSpace(opt.ApprovalMode)); am != "" {
		if am != "manual" && am != "auto" {
			return fmt.Errorf("sandbox create: --approval-mode must be manual|auto")
		}
		opt.Labels["cautem.approval-mode"] = am
	}

	spec := driver.Spec{
		Name:                     name,
		Image:                    opt.Image,
		Workspace:                ws,
		Command:                  canonicalArgv,
		TTY:                      canonicalMainTTY(canonicalArgv, createTTY),
		IKnow:                    opt.IKnow,
		Env:                      hostEnvForPolicy(doc),
		PolicyPath:               policyPath,
		NoHarden:                 opt.NoHarden,
		Labels:                   opt.Labels,
		PersistVolume:            !opt.NoVolume,
		MainProcessRestartPolicy: opt.RestartPolicy,
		DriverConfigJSON:         opt.DriverConfigJSON,
	}
	applyCreateResources(&spec, opt, memoryBytes)
	for _, p := range opt.Forwards {
		if p > 0 {
			spec.PublishPorts = append(spec.PublishPorts, driver.PortPublish{Host: p, Guest: p})
		}
	}
	for k, v := range opt.Env {
		spec.Env = append(spec.Env, k+"="+v)
	}
	if !opt.NoHostInternal {
		spec.ExtraHosts = driver.HostGatewayExtraHosts()
	}
	spec.GatewayURL = gwURL
	displayURL := ""
	displayPass := ""
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
			if spec.GPU {
				spec.Image = defaults.ImageGUIGPU
			} else {
				spec.Image = defaults.ImageGUI
			}
		}
		displayPass = pass
		displayURL = display.NoVNCURL("127.0.0.1", port, pass)
	}
	if !opt.NoHarden {
		initBin, err := ensureInitBin(ctx)
		if err != nil {
			return err
		}
		spec.InitBin = initBin
	}
	if spec.MainProcessRestartPolicy != "" && spec.MainProcessRestartPolicy != "never" {
		supervisorBin, err := ensureSupervisorBin(ctx)
		if err != nil {
			return fmt.Errorf("sandbox create restart policy: %w", err)
		}
		spec.SupervisorBin = supervisorBin
	}
	if !opt.NoProxy {
		bin, err := ensureProxyBin(ctx)
		if err != nil {
			return err
		}
		spec.ProxyBin = bin
		if err := a.attachSupervisor(ctx, &spec, doc, name, gwURL, attached); err != nil {
			return err
		}
	}
	h, err := a.Sandboxes.Create(ctx, sandbox.CreateOptions{
		Spec:   spec,
		Policy: doc,
	})
	if err != nil {
		log.Error("failed to create sandbox", slogx.Err(err))
		return err
	}
	a.rememberLastSandbox(h.Name, gwURL)
	log = log.With(slog.String("sandbox_id", shortID(string(h.ID))), slog.String("image", h.Image))
	if gwURL != "" {
		cli := a.clientFor(gwURL)
		_, profileWorkspace := a.profileScope()
		baseYAML := ""
		if b, err := os.ReadFile(basePath); err == nil {
			baseYAML = string(b)
		}
		_ = cli.UpsertSandbox(ctx, cautem.Sandbox{
			Name:              h.Name,
			ID:                string(h.ID),
			Image:             h.Image,
			Workspace:         profileWorkspace,
			Network:           h.Network,
			Status:            "running",
			Labels:            opt.Labels,
			BasePolicyYAML:    baseYAML,
			AttachedProviders: attached,
		})
	}
	notes := []string{}
	if !opt.NoProxy {
		notes = append(notes, "proxy=sidecar")
	} else {
		notes = append(notes, "proxy=off")
	}
	if !opt.NoHarden {
		notes = append(notes, "harden=cautem-init")
	} else {
		notes = append(notes, "harden=off")
	}
	if displayURL != "" {
		notes = append(notes, "display=novnc")
	}
	if spec.GPU {
		notes = append(notes, "gpu=cdi")
	}
	if gwURL != "" {
		notes = append(notes, "gateway=registered")
	}
	if len(opt.Providers) > 0 {
		notes = append(notes, "providers="+strings.Join(opt.Providers, ","))
	}
	log.Info("sandbox created", slog.String("notes", strings.Join(notes, " ")))
	fmt.Printf("sandbox create: ok name=%s id=%s network=%s image=%s %s\n",
		h.Name, shortID(string(h.ID)), h.Network, h.Image, strings.Join(notes, " "))
	ui.Ok("sandbox %s ready (%s)", h.Name, strings.Join(notes, " "))
	if displayURL != "" {
		fmt.Printf("display: %s\n", displayURL)
		fmt.Printf("display password: %s\n", displayPass)
		if opt.OpenDisplay {
			if err := display.OpenHostBrowser(displayURL); err != nil {
				fmt.Fprintf(os.Stderr, "display: open browser: %v\n", err)
			}
		}
	}
	if spec.EnableSSH {
		fmt.Printf("ssh: via gateway relay (cautem sandbox connect %s | --editor cursor|vscode)\n", h.Name)
	}
	if spec.GPU {
		fmt.Printf("gpu: CDI DeviceRequests enabled (see docs/exp/GPU.md)\n")
	}
	if !opt.NoVolume {
		fmt.Printf("volume: cautem-data-%s → %s (retained across stop/start)\n", h.Name, defaults.GuestData)
	}
	if opt.Upload != "" {
		if err := a.copy(ctx, preparedUpload, h.Name+":"+uploadDest); err != nil {
			return err
		}
	}
	if err := a.installAgentConfig(h); err != nil {
		if len(canonicalArgv) == 0 {
			return fmt.Errorf("sandbox create supervisor guidance: %w", err)
		}
		info, inspectErr := a.Sandboxes.Driver.Inspect(ctx, string(h.ID))
		if inspectErr != nil || strings.EqualFold(info.Status, "running") {
			return fmt.Errorf("sandbox create supervisor guidance: %w", err)
		}
		log.Debug("main process exited before optional guidance could be installed", slogx.Err(err))
	}
	if ed := strings.ToLower(strings.TrimSpace(opt.Editor)); ed != "" {
		if err := a.SandboxConnect(ConnectOpts{Name: h.Name, Editor: ed}); err != nil {
			fmt.Fprintf(os.Stderr, "editor: %v\n", err)
		}
	}
	if len(opt.Argv) > 0 {
		if opt.Detach {
			fmt.Printf("sandbox create: --detach skipping exec of %v\n", opt.Argv)
			return nil
		}
		var execErr error
		if createTTY {
			execErr = attachCanonicalMainProcessTTY(ctx, a.Sandboxes.Driver, h.ID, os.Stdin, os.Stdout, int(os.Stdout.Fd()))
		} else if len(canonicalArgv) > 0 {
			execErr = attachCanonicalMainProcess(ctx, a.Sandboxes.Driver, h.ID, os.Stdout)
		} else {
			execErr = a.Exec(ExecOpts{
				Name:  h.Name,
				Argv:  opt.Argv,
				TTY:   createTTY,
				Env:   hostEnvForPolicy(doc),
				Stdin: os.Stdin,
			})
		}
		if opt.NoKeep {
			_ = a.SandboxRemove(h.Name)
			fmt.Printf("sandbox create: --no-keep removed %s\n", h.Name)
		}
		return execErr
	}
	return nil
}

func validateCreateSource(opt SandboxCreateOpts) error {
	if strings.TrimSpace(opt.BuildContext) != "" {
		if opt.From != "" || opt.Image != "" {
			return fmt.Errorf("sandbox create: use either --build or --image/--from")
		}
		return nil
	}
	if opt.Dockerfile != "" {
		return fmt.Errorf("sandbox create: --dockerfile requires --build")
	}
	return nil
}

func sandboxCreateUploadDestination(stagedPath, originalPath string) (string, error) {
	info, err := os.Lstat(stagedPath)
	if err != nil {
		return "", fmt.Errorf("inspect staged upload %q: %w", originalPath, err)
	}
	if info.IsDir() {
		return "/workspace", nil
	}
	return path.Join("/workspace", filepath.Base(originalPath)), nil
}

// SandboxList prints sandboxes.

// SandboxList prints sandboxes.
type SandboxListOpts struct {
	Limit         uint32
	Offset        uint32
	IDs           bool
	Names         bool
	Selector      string
	AllWorkspaces bool
	Output        string
}

// SandboxList prints gateway-registered sandboxes using OpenShell list semantics.

// SandboxList prints gateway-registered sandboxes using OpenShell list semantics.
func (a *App) SandboxList(opt SandboxListOpts) error {
	selector, err := parseSandboxLabelSelector(opt.Selector)
	if err != nil {
		return err
	}
	if opt.Output != "" && opt.Output != "table" && opt.Output != "json" && opt.Output != "yaml" {
		return fmt.Errorf("sandbox list: invalid output %q (expected table, json, or yaml)", opt.Output)
	}
	client, err := a.gatewayClient()
	if err != nil {
		return err
	}
	ctx, cancel := a.withTimeout(TimeoutAPI)
	defer cancel()
	list, err := client.ListControlSandboxes(ctx, a.GlobalWorkspace, opt.AllWorkspaces)
	if err != nil {
		return fmt.Errorf("sandbox list: %w", err)
	}
	filtered := list[:0]
	for _, sb := range list {
		if !opt.AllWorkspaces {
			workspace := sb.Workspace
			if workspace == "" {
				workspace = "default"
			}
			selectedWorkspace := a.GlobalWorkspace
			if selectedWorkspace == "" {
				selectedWorkspace = "default"
			}
			if workspace != selectedWorkspace {
				continue
			}
		}
		matched := true
		for key, value := range selector {
			if sb.Labels[key] != value {
				matched = false
				break
			}
		}
		if matched {
			filtered = append(filtered, sb)
		}
	}
	list = filtered
	if opt.Offset >= uint32(len(list)) {
		list = nil
	} else {
		list = list[opt.Offset:]
	}
	if uint32(len(list)) > opt.Limit {
		list = list[:opt.Limit]
	}
	if opt.IDs || opt.Names {
		for _, sb := range list {
			if opt.IDs {
				fmt.Println(sb.ID)
			} else if opt.AllWorkspaces {
				workspace := sb.Workspace
				if workspace == "" {
					workspace = "default"
				}
				fmt.Printf("%s/%s\n", workspace, sb.Name)
			} else {
				fmt.Println(sb.Name)
			}
		}
		return nil
	}
	switch opt.Output {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(list)
	case "yaml":
		return yaml.NewEncoder(os.Stdout).Encode(list)
	case "", "table":
	default:
	}
	if len(list) == 0 {
		fmt.Println("sandbox list: (none)")
		return nil
	}
	fmt.Printf("%-16s %-12s %-20s %s\n", "NAME", "ID", "STATUS", "IMAGE")
	for _, s := range list {
		fmt.Printf("%-16s %-12s %-20s %s\n", s.Name, shortID(s.ID), s.Status, s.Image)
	}
	return nil
}

func parseSandboxLabelSelector(raw string) (map[string]string, error) {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	pairs := 0
	for _, term := range strings.Split(raw, ",") {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		pairs++
		if pairs > 64 {
			return nil, fmt.Errorf("sandbox list: label selector exceeds 64 pair limit")
		}
		key, value, ok := strings.Cut(term, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok {
			return nil, fmt.Errorf("sandbox list: invalid selector %q (expected key1=value1,key2=value2)", raw)
		}
		if err := validateSandboxLabelKey(key); err != nil {
			return nil, fmt.Errorf("sandbox list: invalid selector %q: %w", raw, err)
		}
		if err := validateSandboxLabelValue(value); err != nil {
			return nil, fmt.Errorf("sandbox list: invalid selector %q: %w", raw, err)
		}
		// The upstream parser inserts into a map; repeated keys use the last value.
		out[key] = value
	}
	return out, nil
}

func validateSandboxLabelKey(key string) error {
	if key == "" {
		return fmt.Errorf("label key cannot be empty")
	}
	if len(key) > 253 {
		return fmt.Errorf("label key exceeds 253 characters")
	}
	prefix, name, hasPrefix := strings.Cut(key, "/")
	if !hasPrefix {
		name = prefix
		prefix = ""
	}
	if name == "" {
		return fmt.Errorf("label key name segment cannot be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("label key name segment exceeds 63 characters")
	}
	if err := validateLabelSegment(name); err != nil {
		return fmt.Errorf("label key name segment: %w", err)
	}
	if !hasPrefix {
		return nil
	}
	if prefix == "" {
		return fmt.Errorf("label key prefix cannot be empty when '/' is present")
	}
	if len(prefix) > 253 {
		return fmt.Errorf("label key prefix exceeds 253 characters")
	}
	for _, r := range prefix {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '.' {
			return fmt.Errorf("label key prefix must be a DNS subdomain")
		}
	}
	if strings.Contains(prefix, "..") || strings.ContainsAny(prefix[:1]+prefix[len(prefix)-1:], "-.") {
		return fmt.Errorf("label key prefix cannot start or end with '-' or '.', or contain consecutive dots")
	}
	return nil
}

func validateSandboxLabelValue(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 63 {
		return fmt.Errorf("label value exceeds 63 characters")
	}
	return validateLabelSegment(value)
}

func validateLabelSegment(value string) error {
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-' && r != '_' && r != '.' {
			return fmt.Errorf("contains invalid characters")
		}
	}
	first, _ := utf8.DecodeRuneInString(value)
	last, _ := utf8.DecodeLastRuneInString(value)
	if !unicode.IsLetter(first) && !unicode.IsNumber(first) {
		return fmt.Errorf("must start with alphanumeric character")
	}
	if !unicode.IsLetter(last) && !unicode.IsNumber(last) {
		return fmt.Errorf("must end with alphanumeric character")
	}
	return nil
}

// ListSandboxes returns sandbox infos for TUI / automation.

// ListSandboxes returns sandbox infos for TUI / automation.
func (a *App) ListSandboxes() ([]driver.Info, error) {
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return nil, fmt.Errorf("sandbox list: docker not available")
	}
	ctx, cancel := a.withTimeout(TimeoutAPI)
	defer cancel()
	return a.Sandboxes.List(ctx)
}

// SandboxStatus prints one sandbox.

// SandboxStatus prints one sandbox.
func (a *App) SandboxStatus(nameOrID string) error {
	ctx, cancel := a.withTimeout(TimeoutAPI)
	defer cancel()
	if gw, err := a.currentGatewayURL(); err == nil && gw != "" {
		sandbox, err := cautem.NewWithToken(gw, a.gatewayTokenForURL(gw)).GetControlSandbox(ctx, a.GlobalWorkspace, nameOrID)
		if err != nil {
			return fmt.Errorf("sandbox get: %w", err)
		}
		fmt.Printf("name:           %s\n", sandbox.Name)
		fmt.Printf("id:             %s\n", sandbox.ID)
		fmt.Printf("workspace:      %s\n", sandbox.Workspace)
		fmt.Printf("registry_status: %s\n", sandbox.Status)
		fmt.Printf("runtime_status:  unavailable\n")
		fmt.Printf("image:          %s\n", sandbox.Image)
		return nil
	}
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("sandbox status: docker not available")
	}
	info, err := a.Sandboxes.Driver.Inspect(ctx, nameOrID)
	if err != nil {
		return err
	}
	fmt.Printf("name:    %s\n", info.Name)
	fmt.Printf("id:      %s\n", info.ID)
	fmt.Printf("status:  %s\n", info.Status)
	fmt.Printf("network: %s\n", info.Network)
	fmt.Printf("image:   %s\n", info.Image)
	return nil
}

// SandboxRemove deletes a sandbox.

// SandboxRemove deletes a sandbox.
func (a *App) SandboxRemove(nameOrID string) error {
	const op = "cli.sandbox.remove"
	log := a.op(op, slog.String("sandbox", nameOrID))
	log.Info("removing sandbox")
	ctx, cancel := a.withTimeout(TimeoutWait)
	defer cancel()
	client, remote, err := a.lifecycleGatewaySandbox(ctx, nameOrID)
	if err != nil {
		return fmt.Errorf("sandbox rm: %w", err)
	}
	if remote {
		if err := client.DeleteControlSandbox(ctx, a.workspaceName(), nameOrID); err != nil {
			log.Error("gateway failed to delete sandbox", slogx.Err(err))
			return fmt.Errorf("sandbox rm: %w", err)
		}
		a.clearLastSandbox(nameOrID)
		log.Info("sandbox removed by gateway")
		fmt.Printf("sandbox rm: ok %s\n", nameOrID)
		return nil
	}
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		err := fmt.Errorf("sandbox rm: docker not available")
		log.Error("docker unavailable", slogx.Err(err))
		return err
	}
	name := nameOrID
	if info, err := a.Sandboxes.Driver.Inspect(ctx, nameOrID); err == nil && info.Name != "" {
		name = info.Name
	}
	if err := a.Sandboxes.Remove(ctx, nameOrID); err != nil {
		log.Error("failed to remove sandbox", slogx.Err(err))
		return err
	}
	if client == nil {
		if u, err := a.currentGatewayURL(); err == nil {
			client = a.clientFor(u)
		}
	}
	if client != nil {
		if err := client.DeleteSandbox(ctx, name); err != nil {
			log.Warn("sandbox removed locally but registry cleanup failed", slogx.Err(err))
		}
	}
	a.clearLastSandbox(name)
	log.Info("sandbox removed", slog.String("name", name))
	fmt.Printf("sandbox rm: ok %s\n", nameOrID)
	return nil
}

// SandboxDelete removes one or more sandboxes, or all registered sandboxes in
// the selected workspace. The registry is the source of truth for --all since
// local runtime listings do not carry workspace ownership metadata.
func (a *App) SandboxDelete(names []string, all bool) error {
	if all {
		client, err := a.gatewayClient()
		if err != nil {
			return fmt.Errorf("sandbox delete --all: %w", err)
		}
		ctx, cancel := a.withTimeout(TimeoutAPI)
		registered, err := client.ListControlSandboxes(ctx, a.GlobalWorkspace, false)
		cancel()
		if err != nil {
			return fmt.Errorf("sandbox delete --all: list workspace sandboxes: %w", err)
		}
		workspace := a.GlobalWorkspace
		if workspace == "" {
			workspace = "default"
		}
		names = names[:0]
		for _, sandbox := range registered {
			sandboxWorkspace := sandbox.Workspace
			if sandboxWorkspace == "" {
				sandboxWorkspace = "default"
			}
			if sandboxWorkspace == workspace {
				names = append(names, sandbox.Name)
			}
		}
		if len(names) == 0 {
			fmt.Println("No sandboxes to delete.")
			return nil
		}
	}
	return removeSandboxes(names, a.SandboxRemove)
}

func removeSandboxes(names []string, remove func(string) error) error {
	var failures []error
	for _, name := range names {
		if err := remove(name); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", name, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("sandbox delete: failed to remove %d of %d sandboxes: %w", len(failures), len(names), errors.Join(failures...))
	}
	return nil
}

// LogsOpts configures observation log streaming.

// LogsOpts configures observation log streaming.
type LogsOpts struct {
	Names    []string
	Follow   bool
	All      bool
	Lines    uint32
	LinesSet bool
	Since    string
	Source   string
	Level    string
}

// Logs streams sandbox container logs to stdout.

// Logs streams sandbox container logs to stdout.
func (a *App) Logs(name string, follow bool) error {
	return a.LogsOpts(LogsOpts{Names: []string{name}, Follow: follow})
}

// LogsOpts streams observation logs (gateway SSE preferred; docker fallback).

// LogsOpts streams observation logs (gateway SSE preferred; docker fallback).
func (a *App) LogsOpts(opt LogsOpts) error {
	return a.LogsToOpts(a.CommandContext(), opt, os.Stdout)
}

// LogsTo streams sandbox container logs to w (used by `cautem term` live observation panel).

// LogsTo streams sandbox container logs to w (used by `cautem term` live observation panel).
func (a *App) LogsTo(ctx context.Context, name string, follow bool, w io.Writer) error {
	return a.LogsToOpts(ctx, LogsOpts{Names: []string{name}, Follow: follow}, w)
}

// LogsToOpts streams one or more sandboxes.

// LogsToOpts streams one or more sandboxes.
func (a *App) LogsToOpts(ctx context.Context, opt LogsOpts, w io.Writer) error {
	if w == nil {
		w = os.Stdout
	}
	if ctx == nil {
		ctx = a.CommandContext()
	}
	if !opt.LinesSet && opt.Lines == 0 {
		opt.Lines = 200
	}
	names := opt.Names
	if len(names) == 0 && !opt.All {
		name, err := a.resolveSandboxTarget("")
		if err != nil {
			return fmt.Errorf("logs: %w", err)
		}
		names = []string{name}
	}
	// Prefer the authorized workspace-scoped control API when a gateway is selected.
	if gw, err := a.currentGatewayURL(); err == nil && gw != "" {
		c := cautem.NewWithToken(gw, a.gatewayTokenForURL(gw))
		if _, err := c.Healthz(ctx); err == nil {
			var sandboxes []cautem.Sandbox
			if opt.All {
				sandboxes, err = c.ListControlSandboxes(ctx, "", true)
				if err != nil {
					return fmt.Errorf("logs: list visible sandboxes: %w", err)
				}
			} else {
				workspace := a.GlobalWorkspace
				if workspace == "" {
					workspace = "default"
				}
				sandboxes = make([]cautem.Sandbox, 0, len(names))
				for _, name := range names {
					sandboxes = append(sandboxes, cautem.Sandbox{Name: name, Workspace: workspace})
				}
			}
			if opt.Follow {
				return c.WatchControlSandboxSet(ctx, sandboxes, opt.Lines, opt.Since, gatewayLogSource(opt.Source), opt.Level, w)
			}
			for _, sandbox := range sandboxes {
				lines, err := c.GetControlSandboxLogsFiltered(ctx, sandbox.Workspace, sandbox.Name, opt.Lines, opt.Since, gatewayLogSource(opt.Source), opt.Level)
				if err != nil {
					return fmt.Errorf("logs: workspace %q sandbox %q: %w", sandbox.Workspace, sandbox.Name, err)
				}
				for _, line := range lines {
					prefix := sandbox.Name
					if opt.All {
						prefix = sandbox.Workspace + "/" + sandbox.Name
					}
					if len(sandboxes) == 1 {
						prefix = line.Source
						if prefix == "" {
							prefix = "proxy"
						}
					}
					fmt.Fprintf(w, "[%s] %s\n", prefix, line.Text)
				}
			}
			return nil
		}
	}
	if opt.All {
		list, err := a.ListSandboxes()
		if err != nil {
			return err
		}
		names = make([]string, 0, len(list))
		for _, sandbox := range list {
			names = append(names, sandbox.Name)
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("logs: no sandbox names")
	}
	// Docker fallback.
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return fmt.Errorf("logs: docker not available")
	}
	if opt.Level != "" {
		return fmt.Errorf("logs: --level is available only through the gateway")
	}
	if !opt.Follow {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, TimeoutWait)
		defer cancel()
	}
	if len(names) > 1 {
		var mu sync.Mutex
		errCh := make(chan error, len(names))
		for _, n := range names {
			go func() {
				pr, pw := io.Pipe()
				go func() {
					info, err := a.Sandboxes.Driver.Inspect(ctx, n)
					if err != nil {
						_ = pw.CloseWithError(err)
						return
					}
					err = streamSandboxLogs(ctx, a.Sandboxes.Driver, info.ID, opt, pw)
					_ = pw.CloseWithError(err)
				}()
				sc := bufio.NewScanner(pr)
				for sc.Scan() {
					mu.Lock()
					_, _ = fmt.Fprintf(w, "[%s] %s\n", n, sc.Text())
					mu.Unlock()
				}
				errCh <- sc.Err()
			}()
		}
		var first error
		for range names {
			if err := <-errCh; err != nil && first == nil {
				first = err
			}
		}
		return first
	}
	info, err := a.Sandboxes.Driver.Inspect(ctx, names[0])
	if err != nil {
		return err
	}
	return streamSandboxLogs(ctx, a.Sandboxes.Driver, info.ID, opt, w)
}

func gatewayLogSource(source string) string {
	if source == "all" {
		return ""
	}
	return source
}

func streamSandboxLogs(ctx context.Context, compute driver.ComputeDriver, id core.ID, opt LogsOpts, w io.Writer) error {
	if logsDriver, ok := compute.(driver.LogOptionsDriver); ok {
		return logsDriver.LogsWithOptions(ctx, id, driver.LogOptions{
			Follow: opt.Follow, Tail: opt.Lines, TailSet: true, Since: opt.Since, Source: opt.Source,
		}, w)
	}
	if opt.Since != "" || opt.Source != "" && opt.Source != "all" {
		return fmt.Errorf("logs: selected driver does not support --since or --source")
	}
	return compute.Logs(ctx, id, opt.Follow, w)
}

// GatewayAdd registers a named gateway in config.
