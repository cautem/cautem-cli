// Package cli wires command-line input to CLI services.
package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/cautem/cautem-cli/internal/global"
	"github.com/cautem/cautem-cli/internal/logger"
	"github.com/cautem/cautem-cli/internal/service"
	tuipkg "github.com/cautem/cautem-cli/internal/tui"
	"golang.org/x/term"
)

// Execute runs the CLI with its public command names and argument shapes.
func Execute(ctx context.Context, args []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	g, rest := global.Parse(args)
	a := service.New()
	a.SetCommandContext(ctx)
	a.ApplyGlobal(g)
	return execute(a, rest)
}

func execute(a *service.App, args []string) error {
	if wantsHelp(args) {
		return printHelp(args)
	}
	if len(args) == 0 {
		fmt.Println(a.Banner())
		fmt.Println("commands: sandbox|cp|exec|provider|profile|policy|gateway|logs|term|status|health|init|doctor|whoami|workspace|forward|service|settings|inference|…")
		fmt.Println("run 'cautem --help' for full usage")
		return nil
	}
	log := logger.FromContext(a.CommandContext()).With(slog.String("op", "cli.execute"), slog.String("cmd", args[0]))
	log.Info("running command")
	switch args[0] {
	case "version":
		if len(args) != 1 {
			return fmt.Errorf("usage: cautem version")
		}
		fmt.Println(a.Version())
		return nil
	case "health":
		if len(args) != 1 {
			return fmt.Errorf("usage: cautem health")
		}
		return a.Health()
	case "init":
		return runInit(a, args[1:])
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: cautem status")
		}
		return a.Status()
	case "doctor", "dr":
		return runDoctor(a, args[1:])
	case "policy", "pol":
		return runPolicy(a, args[1:])
	case "sandbox", "sb":
		return runSandbox(a, args[1:])
	case "exec":
		return runExec(a, args[1:])
	case "cp":
		if len(args) != 3 {
			return fmt.Errorf("usage: cautem cp <local-path> <sandbox>:/path OR cautem cp <sandbox>:/path <local-path>")
		}
		return a.Copy(args[1], args[2])
	case "term":
		return runTerm(a, args[1:])
	case "gateway", "gw":
		return runGateway(a, args[1:])
	case "logs", "lg":
		return runLogs(a, args[1:])
	case "provider":
		return runProvider(a, args[1:])
	case "profile":
		return runProvider(a, append([]string{"profile"}, args[1:]...))
	case "whoami":
		if len(args) != 1 {
			return fmt.Errorf("usage: cautem whoami")
		}
		return a.Whoami()
	case "workspace", "ws":
		return runWorkspace(a, args[1:])
	case "settings":
		return runSettings(a, args[1:])
	case "forward", "fwd":
		return runForward(a, args[1:])
	case "service", "svc":
		return runService(a, args[1:])
	case "rule", "rl":
		return runRule(a, args[1:])
	case "inference":
		return runInference(a, args[1:])
	case "completions":
		return runCompletions(args[1:])
	case "ssh-proxy":
		return runSSHProxy(a, args[1:])
	case "proxy":
		// Internal: Docker sidecar entrypoint (`/cautem/cautem proxy --listen … --policy …`).
		return runProxy(a, args[1:])
	case "install":
		return runInstall(a, args[1:])
	default:
		return fmt.Errorf("unknown command %q (try: status|health|init|exec|cp|doctor|sandbox|provider|policy|gateway|logs|workspace|forward|service|settings|whoami|term)", args[0])
	}
}

func runCompletions(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: cautem completions <bash|zsh|fish|powershell>")
	}
	shell := strings.ToLower(args[0])
	switch shell {
	case "bash":
		fmt.Print(completionsBash)
	case "zsh":
		fmt.Print(completionsZsh)
	case "fish":
		fmt.Print(completionsFish)
	case "powershell":
		fmt.Print(completionsPowerShell)
	default:
		return fmt.Errorf("unsupported shell %q (bash|zsh|fish|powershell)", shell)
	}
	return nil
}

func runInit(a *service.App, args []string) error {
	opt := service.InitOpts{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--agent":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: cautem init --agent cursor [--dir DIR] [--force]")
			}
			i++
			opt.Agent = args[i]
		case "--dir":
			if i+1 >= len(args) {
				return fmt.Errorf("usage: cautem init --agent cursor [--dir DIR] [--force]")
			}
			i++
			opt.Dir = args[i]
		case "--force", "-f":
			opt.Force = true
		default:
			return fmt.Errorf("usage: cautem init --agent cursor [--dir DIR] [--force]")
		}
	}
	if strings.TrimSpace(opt.Agent) == "" {
		return fmt.Errorf("usage: cautem init --agent cursor [--dir DIR] [--force]")
	}
	return a.Init(opt)
}

func runTerm(a *service.App, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: cautem term")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("cautem term: requires an interactive terminal")
	}
	act, err := tuipkg.Run(a)
	if err != nil {
		return err
	}
	switch act.Kind {
	case "connect":
		return a.SandboxConnect(service.ConnectOpts{Name: act.Name})
	case "exec":
		return a.Exec(service.ExecOpts{Name: act.Name, Argv: []string{"bash"}, TTY: true})
	case "logs":
		return a.Logs(act.Name, false)
	default:
		return nil
	}
}

// Main is a helper for tests / embedding.

// Main is a helper for tests / embedding.
func Main() {
	if err := Execute(context.Background(), os.Args[1:]); err != nil {
		if code, ok := service.ExitCode(err); ok {
			os.Exit(code)
		}
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parseProfileIO accepts positional paths or IDs and the common file/output flags.
func parseProfileIO(args []string, fileMode bool) (pathOrID, outFmt string, err error) {
	outFmt = "yaml"
	seenPath := false
	seenOutput := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if key, value, ok := strings.Cut(arg, "="); ok {
			switch key {
			case "--file", "--url", "--from":
				if seenPath {
					return "", "", fmt.Errorf("profile source may be specified only once")
				}
				if strings.TrimSpace(value) == "" {
					return "", "", fmt.Errorf("%s needs a path", key)
				}
				pathOrID, seenPath = value, true
				continue
			case "-o", "--output":
				if fileMode {
					return "", "", fmt.Errorf("%s is not supported for this profile command", key)
				}
				if seenOutput {
					return "", "", fmt.Errorf("profile output format may be specified only once")
				}
				outFmt, seenOutput = strings.ToLower(value), true
				if outFmt != "yaml" && outFmt != "json" {
					return "", "", fmt.Errorf("%s needs yaml|json", key)
				}
				continue
			}
		}
		switch arg {
		case "-f", "--file", "--url", "--from":
			if seenPath {
				return "", "", fmt.Errorf("profile source may be specified only once")
			}
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("%s needs a path", arg)
			}
			if strings.TrimSpace(args[i]) == "" || strings.HasPrefix(args[i], "-") {
				return "", "", fmt.Errorf("%s needs a path", arg)
			}
			pathOrID, seenPath = args[i], true
		case "-o", "--output":
			if fileMode {
				return "", "", fmt.Errorf("%s is not supported for this profile command", arg)
			}
			if seenOutput {
				return "", "", fmt.Errorf("profile output format may be specified only once")
			}
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("%s needs yaml|json", arg)
			}
			outFmt = strings.ToLower(args[i])
			if outFmt != "yaml" && outFmt != "json" {
				return "", "", fmt.Errorf("%s needs yaml|json", arg)
			}
			seenOutput = true
		default:
			if strings.HasPrefix(arg, "-") {
				return "", "", fmt.Errorf("unknown flag %q", arg)
			}
			if seenPath {
				return "", "", fmt.Errorf("unexpected extra argument %q", arg)
			}
			pathOrID, seenPath = arg, true
		}
	}
	return pathOrID, outFmt, nil
}
