package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cautem/cautem-cli/internal/osargs"
	"github.com/cautem/cautem-cli/internal/service"
	"github.com/cautem/cautem-cli/internal/storage/templates"
	"golang.org/x/term"
)

func runSandbox(a *service.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cautem sandbox create|list|get|stop|start|delete|exec|connect|upload|download|ssh-config|provider")
	}
	switch args[0] {
	case "create":
		opt := service.SandboxCreateOpts{Workspace: ".", Labels: map[string]string{}}
		rest := args[1:]
		for i := 0; i < len(rest); i++ {
			if rest[i] == "--" {
				opt.Argv = append([]string{}, rest[i+1:]...)
				break
			}
			switch rest[i] {
			case "--name":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--name needs a value")
				}
				opt.Name = rest[i]
			case "--image":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--image needs a value")
				}
				opt.Image = rest[i]
			case "--workspace":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--workspace needs a value")
				}
				opt.Workspace = rest[i]
			case "--policy":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--policy needs a value")
				}
				opt.Policy = rest[i]
			case "--i-know":
				opt.IKnow = true
			case "--no-proxy":
				opt.NoProxy = true
			case "--no-harden":
				opt.NoHarden = true
			case "--display":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--display needs a value (none|novnc)")
				}
				opt.Display = rest[i]
			case "--display-port":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--display-port needs a value")
				}
				var p int
				if _, err := fmt.Sscanf(rest[i], "%d", &p); err != nil || p <= 0 {
					return fmt.Errorf("invalid --display-port")
				}
				opt.DisplayPort = p
			case "--open-display":
				opt.OpenDisplay = true
			case "--label":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--label needs key=value")
				}
				k, v, ok := strings.Cut(rest[i], "=")
				if !ok || k == "" {
					return fmt.Errorf("--label needs key=value")
				}
				opt.Labels[k] = v
			case "--gateway":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--gateway needs a URL")
				}
				opt.GatewayURL = rest[i]
			case "--from":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--from needs an alias")
				}
				opt.From = rest[i]
			case "--ssh":
				return fmt.Errorf("sandbox create: --ssh was removed; SSH relay is configured automatically when a gateway is selected")
			case "--gpu":
				opt.GPU = true
			case "--cdi":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--cdi needs a device id (e.g. nvidia.com/gpu=all)")
				}
				opt.CDIDevices = append(opt.CDIDevices, rest[i])
			case "--no-volume":
				opt.NoVolume = true
			case "--no-host-internal":
				opt.NoHostInternal = true
			case "--provider":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--provider needs a profile id or instance name (e.g. github)")
				}
				opt.Providers = append(opt.Providers, rest[i])
			case "--auto-providers":
				opt.AutoProviders = true
			case "--no-auto-providers":
				opt.NoAutoProviders = true
			case "--no-credential-warnings":
				opt.NoCredentialWarnings = true
			case "--env":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--env needs KEY=VALUE")
				}
				k, v, ok := strings.Cut(rest[i], "=")
				if !ok || k == "" {
					return fmt.Errorf("--env needs KEY=VALUE")
				}
				if opt.Env == nil {
					opt.Env = map[string]string{}
				}
				opt.Env[k] = v
			case "--detach":
				opt.Detach = true
			case "--editor":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--editor needs vscode|cursor")
				}
				opt.Editor = rest[i]
			case "--forward":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--forward needs PORT")
				}
				var p int
				if _, err := fmt.Sscanf(rest[i], "%d", &p); err != nil || p <= 0 {
					return fmt.Errorf("invalid --forward port")
				}
				opt.Forwards = append(opt.Forwards, p)
			case "--upload":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--upload needs PATH")
				}
				opt.Upload = rest[i]
			case "--cpu":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--cpu needs a float")
				}
				var f float64
				if _, err := fmt.Sscanf(rest[i], "%f", &f); err != nil || f <= 0 {
					return fmt.Errorf("invalid --cpu")
				}
				opt.CPU = f
			case "--memory":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--memory needs a size (512m, 4g, …)")
				}
				opt.Memory = rest[i]
			case "--pids-limit":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--pids-limit needs an int (-1 unlimited, >0 limit)")
				}
				var n int64
				if _, err := fmt.Sscanf(rest[i], "%d", &n); err != nil || n == 0 {
					return fmt.Errorf("invalid --pids-limit (use -1 for unlimited or positive int)")
				}
				opt.PidsLimit = n
			case "--template":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--template needs NAME")
				}
				opt.Template = rest[i]
			case "--driver-config-json":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--driver-config-json needs JSON")
				}
				opt.DriverConfigJSON = rest[i]
			case "--approval-mode":
				i++
				if i >= len(rest) {
					return fmt.Errorf("--approval-mode needs manual|auto")
				}
				opt.ApprovalMode = rest[i]
			case "--no-keep":
				opt.NoKeep = true
			case "--tty":
				opt.ForceTTY = true
			default:
				return fmt.Errorf("unknown flag %q", rest[i])
			}
		}
		return a.SandboxCreate(opt)
	case "list", "ls":
		opt := service.SandboxListOpts{Limit: 100, Output: "table"}
		seen := map[string]bool{}
		for i := 1; i < len(args); i++ {
			arg := args[i]
			flag := strings.SplitN(arg, "=", 2)[0]
			if seen[flag] {
				return fmt.Errorf("sandbox list: duplicate flag %q", flag)
			}
			seen[flag] = true
			value := func() (string, error) {
				if eq := strings.IndexByte(arg, '='); eq >= 0 {
					return arg[eq+1:], nil
				}
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					return "", fmt.Errorf("%s needs a value", arg)
				}
				i++
				return args[i], nil
			}
			switch flag {
			case "--limit", "--offset", "--selector", "-o", "--output":
				v, err := value()
				if err != nil {
					return err
				}
				switch flag {
				case "--limit":
					n, err := strconv.ParseUint(v, 10, 32)
					if err != nil {
						return fmt.Errorf("sandbox list: invalid --limit %q", v)
					}
					opt.Limit = uint32(n)
				case "--offset":
					n, err := strconv.ParseUint(v, 10, 32)
					if err != nil {
						return fmt.Errorf("sandbox list: invalid --offset %q", v)
					}
					opt.Offset = uint32(n)
				case "--selector":
					opt.Selector = v
				case "-o", "--output":
					opt.Output = v
				}
			case "--ids":
				opt.IDs = true
			case "--names":
				opt.Names = true
			case "--all-workspaces":
				opt.AllWorkspaces = true
			default:
				return fmt.Errorf("sandbox list: unknown argument %q", arg)
			}
		}
		if opt.IDs && opt.Names {
			return fmt.Errorf("sandbox list: --ids conflicts with --names")
		}
		if (opt.IDs || opt.Names) && seen["-o"] || (opt.IDs || opt.Names) && seen["--output"] {
			return fmt.Errorf("sandbox list: --ids/--names conflict with --output")
		}
		return a.SandboxList(opt)
	case "get", "status":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem sandbox get <name>")
		}
		return a.SandboxStatus(args[1])
	case "stop":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem sandbox stop <name>")
		}
		return a.SandboxStop(args[1])
	case "start":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem sandbox start <name>")
		}
		return a.SandboxStart(args[1])
	case "rm", "remove", "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem sandbox delete <name>")
		}
		return a.SandboxRemove(args[1])
	case "exec":
		return runExec(a, args[1:])
	case "upload", "download":
		parsed, err := osargs.ParseSandboxTransfer(args[1:])
		if err != nil {
			return err
		}
		dest := parsed.Dest
		if args[0] == "upload" {
			if dest == "" {
				dest = "/workspace"
			}
			return a.Copy(parsed.Path, parsed.Name+":"+dest)
		}
		if dest == "" {
			dest = "."
		}
		return a.Copy(parsed.Name+":"+parsed.Path, dest)
	case "ssh-config":
		parsed, err := osargs.ParseSandboxSSHConfig(args[1:])
		if err != nil {
			return err
		}
		return a.SandboxSSHConfig(parsed.Name, parsed.Install)
	case "connect":
		parsed, err := osargs.ParseSandboxConnect(args[1:])
		if err != nil {
			return err
		}
		return a.SandboxConnect(service.ConnectOpts{Name: parsed.Name, Editor: parsed.Editor, Argv: parsed.Argv})
	case "template":
		return runSandboxTemplate(a, args[1:])
	case "provider":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem sandbox provider list|attach|detach …")
		}
		switch args[1] {
		case "list", "ls":
			if len(args) < 3 {
				return fmt.Errorf("usage: cautem sandbox provider list <sandbox>")
			}
			return a.SandboxProviderList(args[2])
		case "attach":
			if len(args) < 4 {
				return fmt.Errorf("usage: cautem sandbox provider attach <sandbox> <provider>")
			}
			return a.ProviderAttach(args[2], args[3])
		case "detach":
			if len(args) < 4 {
				return fmt.Errorf("usage: cautem sandbox provider detach <sandbox> <provider>")
			}
			return a.ProviderDetach(args[2], args[3])
		default:
			return fmt.Errorf("unknown sandbox provider subcommand %q", args[1])
		}
	default:
		return fmt.Errorf("unknown sandbox subcommand %q", args[0])
	}
}

func runExec(a *service.App, args []string) error {
	parsed, err := osargs.ParseSandboxExec(args)
	if err != nil {
		return err
	}
	env := make([]string, 0, len(parsed.Env))
	for k, v := range parsed.Env {
		env = append(env, k+"="+v)
	}
	return a.Exec(service.ExecOpts{
		Name:    parsed.Name,
		Argv:    parsed.Argv,
		TTY:     term.IsTerminal(int(os.Stdin.Fd())),
		Env:     env,
		WorkDir: parsed.WorkDir,
	})
}

func runSandboxTemplate(a *service.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cautem sandbox template create|list|get|delete …")
	}
	switch args[0] {
	case "create":
		t := templates.Template{}
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--name":
				i++
				if i >= len(args) {
					return fmt.Errorf("--name required")
				}
				t.Name = args[i]
			case "--image":
				i++
				if i >= len(args) {
					return fmt.Errorf("--image needs value")
				}
				t.Image = args[i]
			case "--from":
				i++
				if i >= len(args) {
					return fmt.Errorf("--from needs value")
				}
				t.From = args[i]
			case "--policy":
				i++
				if i >= len(args) {
					return fmt.Errorf("--policy needs path")
				}
				t.Policy = args[i]
			case "--cpu":
				i++
				if i >= len(args) {
					return fmt.Errorf("--cpu needs value")
				}
				if _, err := fmt.Sscanf(args[i], "%f", &t.CPU); err != nil {
					return fmt.Errorf("invalid --cpu")
				}
			case "--memory":
				i++
				if i >= len(args) {
					return fmt.Errorf("--memory needs value")
				}
				t.Memory = args[i]
			case "--pids-limit":
				i++
				if i >= len(args) {
					return fmt.Errorf("--pids-limit needs value")
				}
				if _, err := fmt.Sscanf(args[i], "%d", &t.PidsLimit); err != nil || t.PidsLimit == 0 {
					return fmt.Errorf("invalid --pids-limit")
				}
			case "--provider":
				i++
				if i >= len(args) {
					return fmt.Errorf("--provider needs value")
				}
				t.Providers = append(t.Providers, args[i])
			default:
				return fmt.Errorf("unknown flag %q", args[i])
			}
		}
		if t.Name == "" {
			return fmt.Errorf("template create: --name required")
		}
		return a.TemplateCreate(t)
	case "list", "ls":
		return a.TemplateList()
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem sandbox template get <name>")
		}
		return a.TemplateGet(args[1])
	case "delete", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem sandbox template delete <name>")
		}
		return a.TemplateDelete(args[1])
	default:
		return fmt.Errorf("unknown template subcommand %q", args[0])
	}
}

// runSSHProxy is the OpenShell ProxyCommand hook (token / name / legacy modes).
func runSSHProxy(a *service.App, args []string) error {
	parsed, err := osargs.ParseSSHProxy(args)
	if err != nil {
		return err
	}
	return a.SSHProxy(service.SSHProxyOpts{
		GatewayURL:  parsed.GatewayURL,
		GatewayName: parsed.GatewayName,
		SandboxID:   parsed.SandboxID,
		Token:       parsed.Token,
		Name:        parsed.Name,
	})
}

// runProxy is the sidecar / host CONNECT proxy entrypoint.
// Usage: cautem proxy --listen ADDR --policy PATH [--ca-out PATH]

// runProxy is the sidecar / host CONNECT proxy entrypoint.
// Usage: cautem proxy --listen ADDR --policy PATH [--ca-out PATH]
func runProxy(a *service.App, args []string) error {
	opt := service.ProxyOpts{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--listen", "-l":
			if i+1 >= len(args) {
				return fmt.Errorf("proxy: --listen needs a value")
			}
			i++
			opt.Listen = args[i]
		case "--policy", "-p":
			if i+1 >= len(args) {
				return fmt.Errorf("proxy: --policy needs a value")
			}
			i++
			opt.Policy = args[i]
		case "--ca-out":
			if i+1 >= len(args) {
				return fmt.Errorf("proxy: --ca-out needs a value")
			}
			i++
			opt.CAOut = args[i]
		case "--gateway":
			if i+1 >= len(args) {
				return fmt.Errorf("proxy: --gateway needs a value")
			}
			i++
			opt.GatewayURL = args[i]
		case "--sandbox":
			if i+1 >= len(args) {
				return fmt.Errorf("proxy: --sandbox needs a value")
			}
			i++
			opt.Sandbox = args[i]
		case "--log-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("proxy: --log-dir needs a value")
			}
			i++
			opt.LogDir = args[i]
		case "--help", "-h":
			fmt.Fprintln(os.Stderr, "usage: cautem proxy --listen ADDR --policy PATH [--ca-out PATH] [--gateway URL] [--sandbox NAME]")
			return nil
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("proxy: unknown flag %q", args[i])
			}
			return fmt.Errorf("proxy: unexpected arg %q", args[i])
		}
	}
	if strings.TrimSpace(opt.Policy) == "" {
		return fmt.Errorf("usage: cautem proxy --listen ADDR --policy PATH [--ca-out PATH]")
	}
	return a.Proxy(opt)
}
