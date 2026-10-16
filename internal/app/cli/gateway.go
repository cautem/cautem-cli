package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cautem/quarshell-cli/internal/osargs"
	"github.com/cautem/quarshell-cli/internal/service"
)

func runGateway(a *service.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cautem gateway ensure|add|remove|select|info|list|login|logout")
	}
	switch args[0] {
	case "ensure":
		if len(args) != 1 {
			return fmt.Errorf("usage: cautem gateway ensure")
		}
		return a.GatewayEnsure()
	case "add":
		parsed, err := osargs.ParseGatewayAdd(args[1:])
		if err != nil {
			return err
		}
		return a.GatewayAddParsed(parsed)
	case "remove", "rm", "delete":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return fmt.Errorf("usage: cautem gateway remove [name]")
		}
		return a.GatewayRemove(args[1])
	case "select":
		if len(args) == 1 {
			return a.GatewayListRemote() // Selecting without a name lists registered gateways.
		}
		if len(args) != 2 {
			return fmt.Errorf("usage: cautem gateway select [name]")
		}
		return a.GatewaySelect(args[1])
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: cautem gateway status")
		}
		return a.GatewayStatus()
	case "info":
		if len(args) != 1 {
			return fmt.Errorf("usage: cautem gateway info")
		}
		return a.GatewayInfo()
	case "list", "ls":
		if len(args) != 1 {
			return fmt.Errorf("usage: cautem gateway list")
		}
		return a.GatewayListRemote()
	case "login":
		if len(args) > 2 {
			return fmt.Errorf("usage: cautem gateway login [token]")
		}
		token := ""
		if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
			token = args[1]
		}
		return a.GatewayLoginInteractive(token)
	case "logout":
		if len(args) != 1 {
			return fmt.Errorf("usage: cautem gateway logout")
		}
		return a.GatewayLogout()
	default:
		return fmt.Errorf("unknown gateway subcommand %q", args[0])
	}
}

func runWorkspace(a *service.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cautem workspace create|get|list|delete|member …")
	}
	switch args[0] {
	case "create":
		name, labels, err := parseWorkspaceCreateArgs(args[1:])
		if err != nil {
			return err
		}
		return a.WorkspaceCreate(name, labels)
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem workspace get <name>")
		}
		if len(args) > 2 {
			return fmt.Errorf("unexpected argument %q", args[2])
		}
		return a.WorkspaceGet(args[1])
	case "list", "ls":
		options, err := parseWorkspaceListArgs(args[1:])
		if err != nil {
			return err
		}
		return a.WorkspaceList(options)
	case "delete", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem workspace delete <name> [name ...]")
		}
		names, err := parseWorkspaceDeleteNames(args[1:])
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := a.WorkspaceDelete(name); err != nil {
				return err
			}
		}
		return nil
	case "member":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem workspace member add|remove|list …")
		}
		switch args[1] {
		case "add":
			ws, subject, role, err := parseWorkspaceMemberFlags(args[2:], true)
			if err != nil {
				return err
			}
			return a.WorkspaceMemberAddRole(ws, subject, role)
		case "remove", "rm":
			ws, subject, _, err := parseWorkspaceMemberFlags(args[2:], false)
			if err != nil {
				return err
			}
			return a.WorkspaceMemberRemove(ws, subject)
		case "list", "ls":
			ws := ""
			for i := 2; i < len(args); i++ {
				switch args[i] {
				case "--workspace":
					if ws != "" {
						return fmt.Errorf("workspace may be specified only once")
					}
					i++
					if i >= len(args) || strings.TrimSpace(args[i]) == "" || strings.HasPrefix(args[i], "-") {
						return fmt.Errorf("--workspace needs a value")
					}
					ws = args[i]
				default:
					if strings.HasPrefix(args[i], "-") {
						return fmt.Errorf("unknown flag %q", args[i])
					}
					if ws != "" {
						return fmt.Errorf("unexpected argument %q", args[i])
					}
					ws = args[i]
				}
			}
			if ws == "" {
				return fmt.Errorf("usage: cautem workspace member list --workspace NAME")
			}
			return a.WorkspaceMemberList(ws)
		default:
			return fmt.Errorf("unknown member subcommand %q", args[1])
		}
	default:
		return fmt.Errorf("unknown workspace subcommand %q", args[0])
	}
}

func parseWorkspaceCreateArgs(args []string) (string, map[string]string, error) {
	name := ""
	labelArgs := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--name" || strings.HasPrefix(arg, "--name="):
			if name != "" {
				return "", nil, fmt.Errorf("workspace name may be specified only once")
			}
			value, next, err := parseWorkspaceOptionValue(args, i, "--name")
			if err != nil {
				return "", nil, err
			}
			i = next
			name = value
		case arg == "--label" || strings.HasPrefix(arg, "--label="):
			value, next, err := parseWorkspaceOptionValue(args, i, "--label")
			if err != nil {
				return "", nil, err
			}
			i = next
			labelArgs = append(labelArgs, value)
		default:
			if strings.HasPrefix(arg, "-") {
				return "", nil, fmt.Errorf("unknown flag %q", arg)
			}
			if name != "" {
				return "", nil, fmt.Errorf("unexpected argument %q", arg)
			}
			name = arg
		}
	}
	if strings.TrimSpace(name) == "" {
		return "", nil, fmt.Errorf("usage: cautem workspace create --name NAME")
	}
	labels := make(map[string]string, len(labelArgs))
	for _, label := range labelArgs {
		key, value, ok := strings.Cut(label, "=")
		if ok {
			labels[key] = value
		}
	}
	return name, labels, nil
}

func parseWorkspaceListArgs(args []string) (service.WorkspaceListOptions, error) {
	options := service.WorkspaceListOptions{PageSize: 100}
	seen := make(map[string]bool)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag := ""
		switch {
		case arg == "--page-size" || strings.HasPrefix(arg, "--page-size="):
			flag = "--page-size"
		case arg == "--page-token" || strings.HasPrefix(arg, "--page-token="):
			flag = "--page-token"
		case arg == "--label-selector" || strings.HasPrefix(arg, "--label-selector="):
			flag = "--label-selector"
		case arg == "--output" || strings.HasPrefix(arg, "--output=") || arg == "-o":
			flag = "--output"
		default:
			if strings.HasPrefix(arg, "-") {
				return service.WorkspaceListOptions{}, fmt.Errorf("unknown flag %q", arg)
			}
			return service.WorkspaceListOptions{}, fmt.Errorf("unexpected argument %q", arg)
		}
		if seen[flag] {
			return service.WorkspaceListOptions{}, fmt.Errorf("%s may be specified only once", flag)
		}
		seen[flag] = true
		value, next, err := parseWorkspaceOptionValue(args, i, flag)
		if err != nil {
			return service.WorkspaceListOptions{}, err
		}
		i = next
		switch flag {
		case "--page-size":
			pageSize, err := strconv.ParseInt(value, 10, 32)
			if err != nil || pageSize <= 0 {
				return service.WorkspaceListOptions{}, fmt.Errorf("--page-size must be a positive integer")
			}
			options.PageSize = int(pageSize)
		case "--page-token":
			options.PageToken = value
		case "--label-selector":
			options.LabelSelector = value
		case "--output":
			if value != "table" && value != "yaml" && value != "json" {
				return service.WorkspaceListOptions{}, fmt.Errorf("--output must be table|yaml|json")
			}
			options.Output = value
		}
	}
	return options, nil
}

func parseWorkspaceOptionValue(args []string, index int, flag string) (string, int, error) {
	if value, ok := strings.CutPrefix(args[index], flag+"="); ok {
		if flag == "--name" && strings.TrimSpace(value) == "" {
			return "", index, fmt.Errorf("%s needs a value", flag)
		}
		return value, index, nil
	}
	index++
	if index >= len(args) || strings.TrimSpace(args[index]) == "" || strings.HasPrefix(args[index], "-") {
		return "", index, fmt.Errorf("%s needs a value", flag)
	}
	return args[index], index, nil
}

func parseWorkspaceDeleteNames(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: cautem workspace delete <name> [name ...]")
	}
	names := make([]string, 0, len(args))
	for _, name := range args {
		if strings.TrimSpace(name) == "" || strings.HasPrefix(name, "-") {
			return nil, fmt.Errorf("workspace delete needs one or more names")
		}
		names = append(names, name)
	}
	return names, nil
}

// parseWorkspaceMemberFlags accepts named flags or positional workspace/member values.
func parseWorkspaceMemberFlags(args []string, needRole bool) (ws, subject, role string, err error) {
	role = "user"
	var positionals []string
	seenWorkspace, seenSubject, seenRole := false, false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace":
			if seenWorkspace || ws != "" {
				return "", "", "", fmt.Errorf("workspace may be specified only once")
			}
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" || strings.HasPrefix(args[i], "-") {
				return "", "", "", fmt.Errorf("--workspace needs a value")
			}
			ws, seenWorkspace = args[i], true
		case "--subject":
			if seenSubject || subject != "" {
				return "", "", "", fmt.Errorf("subject may be specified only once")
			}
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" || strings.HasPrefix(args[i], "-") {
				return "", "", "", fmt.Errorf("--subject needs a value")
			}
			subject, seenSubject = args[i], true
		case "--role":
			if !needRole {
				return "", "", "", fmt.Errorf("--role is only valid for workspace member add")
			}
			if seenRole {
				return "", "", "", fmt.Errorf("--role may be specified only once")
			}
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" || strings.HasPrefix(args[i], "-") {
				return "", "", "", fmt.Errorf("--role needs user|admin")
			}
			role, seenRole = args[i], true
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", "", fmt.Errorf("unknown flag %q", args[i])
			}
			positionals = append(positionals, args[i])
		}
	}
	if len(positionals) > 0 && (seenWorkspace || seenSubject) {
		return "", "", "", fmt.Errorf("use either positional workspace/member values or --workspace/--subject flags")
	}
	if ws == "" && len(positionals) > 0 {
		ws = positionals[0]
		positionals = positionals[1:]
	}
	if subject == "" && len(positionals) > 0 {
		subject = positionals[0]
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", "", "", fmt.Errorf("unexpected argument %q", positionals[0])
	}
	if ws == "" || subject == "" {
		return "", "", "", fmt.Errorf("usage: cautem workspace member add --workspace NAME --subject SUBJECT [--role user|admin]")
	}
	if needRole && role != "user" && role != "admin" {
		return "", "", "", fmt.Errorf("--role must be user|admin")
	}
	return ws, subject, role, nil
}

func runSettings(a *service.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cautem settings get|set|delete …")
	}
	switch args[0] {
	case "get":
		name := ""
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--global", "--json":
			default:
				if strings.HasPrefix(args[i], "-") {
					return fmt.Errorf("unknown flag %q", args[i])
				}
				if name != "" {
					return fmt.Errorf("unexpected argument %q", args[i])
				}
				name = args[i]
			}
		}
		if name == "" {
			return a.SettingsGet("")
		}
		return a.SettingsGet(name)
	case "set":
		parsed, err := osargs.ParseSettingsSet(args[1:])
		if err != nil {
			return err
		}
		return a.SettingsSet(parsed.Key, parsed.Value)
	case "delete", "rm":
		key := ""
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--key":
				if key != "" {
					return fmt.Errorf("settings key specified more than once")
				}
				i++
				if i >= len(args) {
					return fmt.Errorf("--key needs a value")
				}
				key = args[i]
			case "--global":
			default:
				if strings.HasPrefix(args[i], "-") {
					return fmt.Errorf("unknown flag %q", args[i])
				}
				if key != "" {
					return fmt.Errorf("unexpected argument %q", args[i])
				}
				key = args[i]
			}
		}
		if key == "" {
			return fmt.Errorf("usage: cautem settings delete --key KEY [--global]")
		}
		return a.SettingsDelete(key)
	default:
		return fmt.Errorf("unknown settings subcommand %q", args[0])
	}
}
