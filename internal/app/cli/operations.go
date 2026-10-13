package cli

import (
	"fmt"
	"strings"

	"github.com/cautem/cautem-cli/internal/osargs"
	"github.com/cautem/cautem-cli/internal/service"
)

func runForward(a *service.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cautem forward start|stop|list …")
	}
	switch args[0] {
	case "start":
		parsed, err := osargs.ParseForwardStart(args[1:])
		if err != nil {
			return err
		}
		if parsed.Name == "" {
			return fmt.Errorf("usage: cautem forward start [bind:]port <sandbox> [-d]")
		}
		return a.ForwardStart(parsed.Name, parsed.Port, parsed.Port, parsed.Background)
	case "stop":
		parsed, err := osargs.ParseForwardStop(args[1:])
		if err != nil {
			return err
		}
		id := parsed.Port
		if parsed.Name != "" {
			id = parsed.Name + ":" + parsed.Port
		}
		return a.ForwardStop(id)
	case "list", "ls":
		return a.ForwardList()
	case "service":
		return fmt.Errorf("forward service: use cautem service expose")
	default:
		return fmt.Errorf("unknown forward subcommand %q", args[0])
	}
}

func runService(a *service.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cautem service expose|list|get|delete …")
	}
	switch args[0] {
	case "expose":
		parsed, err := osargs.ParseServiceExpose(args[1:])
		if err != nil {
			return err
		}
		return a.ServiceExpose(parsed.Sandbox, parsed.Service, parsed.Port)
	case "list", "ls":
		return a.ServiceList()
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem service get <sandbox> [service]")
		}
		name := "default"
		if len(args) > 2 {
			name = args[2]
		}
		return a.ServiceGet(name)
	case "delete", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem service delete <sandbox> [service]")
		}
		name := "default"
		if len(args) > 2 {
			name = args[2]
		}
		return a.ServiceDelete(name)
	default:
		return fmt.Errorf("unknown service subcommand %q", args[0])
	}
}

func runRule(a *service.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cautem rule get|approve|reject|history|clear …")
	}
	switch args[0] {
	case "get", "list", "ls":
		status := ""
		sandbox := ""
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--status":
				i++
				if i >= len(args) {
					return fmt.Errorf("--status needs pending|approved|rejected")
				}
				status = args[i]
			default:
				if strings.HasPrefix(args[i], "-") {
					return fmt.Errorf("unknown flag %q", args[i])
				}
				sandbox = args[i]
			}
		}
		return a.RuleListFilter(sandbox, status)
	case "history":
		sandbox := ""
		if len(args) > 1 {
			sandbox = args[1]
		}
		return a.RuleListFilter(sandbox, "")
	case "approve":
		id, reason, err := parseRuleAction(args[1:])
		if err != nil {
			return err
		}
		_ = reason
		return a.RuleApprove(id)
	case "approve-all":
		sandbox := ""
		includeSecurityFlagged := false
		for _, arg := range args[1:] {
			switch {
			case arg == "--include-security-flagged":
				includeSecurityFlagged = true
			case strings.HasPrefix(arg, "-"):
				return fmt.Errorf("unknown flag %q", arg)
			case sandbox == "":
				sandbox = arg
			default:
				return fmt.Errorf("usage: cautem rule approve-all [NAME] [--include-security-flagged]")
			}
		}
		return a.RuleApproveAll(sandbox, includeSecurityFlagged)
	case "reject":
		id, reason, err := parseRuleAction(args[1:])
		if err != nil {
			return err
		}
		return a.RuleRejectReason(id, reason)
	case "clear":
		return a.RuleClear()
	default:
		return fmt.Errorf("unknown rule subcommand %q", args[0])
	}
}

func parseRuleAction(args []string) (id, reason string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--chunk-id":
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("--chunk-id needs a value")
			}
			id = args[i]
		case "--reason":
			i++
			if i >= len(args) {
				return "", "", fmt.Errorf("--reason needs a value")
			}
			reason = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", fmt.Errorf("unknown flag %q", args[i])
			}
			if id == "" {
				id = args[i]
			}
		}
	}
	if id == "" {
		return "", "", fmt.Errorf("usage: cautem rule approve|reject [--chunk-id] ID [--reason TEXT]")
	}
	return id, reason, nil
}

func runLogs(a *service.App, args []string) error {
	parsed, err := osargs.ParseLogs(args)
	if err != nil {
		return err
	}
	opt := service.LogsOpts{
		Follow: parsed.Tail,
		All:    parsed.All,
		Since:  parsed.Since,
		Source: parsed.Source,
		Level:  parsed.Level,
	}
	if parsed.Name != "" {
		for n := range strings.SplitSeq(parsed.Name, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				opt.Names = append(opt.Names, n)
			}
		}
	}
	if !opt.All && len(opt.Names) == 0 {
		return fmt.Errorf("usage: cautem logs <name> [--tail] [-n N] [--since 5m] [--source sandbox] [--level warn]")
	}
	return a.LogsOpts(opt)
}

func runInference(a *service.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cautem inference get|set|update|list|show|local")
	}
	switch args[0] {
	case "get":
		return a.InferenceRouteGet()
	case "set", "update":
		parsed, err := osargs.ParseInferenceSet(args[1:])
		if err != nil {
			return err
		}
		return a.InferenceRouteSet(parsed)
	case "list", "ls":
		return a.InferenceList()
	case "show":
		path := ""
		if len(args) > 1 {
			path = args[1]
		}
		return a.InferenceShow(path)
	case "local":
		return a.InferenceLocal()
	case "delete", "rm":
		return a.InferenceRouteDelete()
	default:
		return fmt.Errorf("unknown inference subcommand %q", args[0])
	}
}
