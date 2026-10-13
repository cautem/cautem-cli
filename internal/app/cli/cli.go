// Package cli wires command-line input to CLI services.
package cli

import (
	"context"
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

// Execute runs the CLI with OpenShell command names and argv shapes.
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
		fmt.Println("commands: sandbox|exec|provider|profile|policy|gateway|logs|term|status|health|init|doctor|whoami|workspace|forward|service|settings|inference|…")
		fmt.Println("run 'cautem --help' for full usage")
		return nil
	}
	log := logger.FromContext(a.CommandContext()).With(slog.String("op", "cli.execute"), slog.String("cmd", args[0]))
	log.Info("running command")
	switch args[0] {
	case "version":
		fmt.Println(a.Version())
		return nil
	case "health":
		return a.Health()
	case "init":
		return runInit(a, args[1:])
	case "status":
		return a.Status()
	case "doctor", "dr":
		return runDoctor(a, args[1:])
	case "policy", "pol":
		return runPolicy(a, args[1:])
	case "sandbox", "sb":
		return runSandbox(a, args[1:])
	case "exec":
		return runExec(a, args[1:])
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
		return fmt.Errorf("unknown command %q (try: status|health|init|exec|doctor|sandbox|provider|policy|gateway|logs|workspace|forward|service|settings|whoami|term)", args[0])
	}
}

func runCompletions(args []string) error {
	if len(args) == 0 {
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

func runTerm(a *service.App, _ []string) error {
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parseProfileIO accepts positional path/id and OpenShell -f/--file, -o/--output.

// parseProfileIO accepts positional path/id and OpenShell -f/--file, -o/--output.
func parseProfileIO(args []string, fileMode bool) (pathOrID, outFmt string, err error) {
	outFmt = "yaml"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-f", "--file", "--url", "--from":
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("%s needs a path", args[i-1])
			}
			pathOrID = args[i]
		case "-o", "--output":
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("%s needs yaml|json", args[i-1])
			}
			outFmt = strings.ToLower(args[i])
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", fmt.Errorf("unknown flag %q", args[i])
			}
			if pathOrID == "" {
				pathOrID = args[i]
			}
		}
	}
	_ = fileMode
	return pathOrID, outFmt, nil
}

// runSSHProxy is the OpenShell ProxyCommand hook (token / name / legacy modes).
