package cli

import (
	"fmt"
	"strings"

	"github.com/cautem/cautem-cli/internal/osargs"
	"github.com/cautem/cautem-cli/internal/service"
)

func runGateway(a *service.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cautem gateway ensure|add|remove|select|info|list|login|logout")
	}
	switch args[0] {
	case "ensure":
		return a.GatewayEnsure()
	case "add":
		parsed, err := osargs.ParseGatewayAdd(args[1:])
		if err != nil {
			return err
		}
		return a.GatewayAddParsed(parsed)
	case "remove", "rm", "delete":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		if name == "" {
			return fmt.Errorf("usage: cautem gateway remove [name]")
		}
		return a.GatewayRemove(name)
	case "select":
		if len(args) < 2 {
			return a.GatewayListRemote() // OpenShell: select without name lists
		}
		return a.GatewaySelect(args[1])
	case "status":
		return a.GatewayStatus()
	case "info":
		return a.GatewayInfo()
	case "list", "ls":
		return a.GatewayListRemote()
	case "login":
		token := ""
		if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
			token = args[1]
		}
		return a.GatewayLoginInteractive(token)
	case "logout":
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
		name := ""
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--name":
				i++
				if i >= len(args) {
					return fmt.Errorf("--name needs a value")
				}
				name = args[i]
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
			return fmt.Errorf("usage: cautem workspace create --name NAME")
		}
		return a.WorkspaceCreate(name)
	case "get":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem workspace get <name>")
		}
		return a.WorkspaceGet(args[1])
	case "list", "ls":
		return a.WorkspaceList()
	case "delete", "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: cautem workspace delete <name>")
		}
		return a.WorkspaceDelete(args[1])
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
					i++
					if i >= len(args) {
						return fmt.Errorf("--workspace needs a value")
					}
					ws = args[i]
				default:
					if strings.HasPrefix(args[i], "-") {
						return fmt.Errorf("unknown flag %q", args[i])
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

// parseWorkspaceMemberFlags accepts OpenShell `--workspace --subject --role` or positional WS SUBJECT.

// parseWorkspaceMemberFlags accepts OpenShell `--workspace --subject --role` or positional WS SUBJECT.
func parseWorkspaceMemberFlags(args []string, needRole bool) (ws, subject, role string, err error) {
	role = "user"
	var positionals []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace":
			i++
			if i >= len(args) {
				return "", "", "", fmt.Errorf("--workspace needs a value")
			}
			ws = args[i]
		case "--subject":
			i++
			if i >= len(args) {
				return "", "", "", fmt.Errorf("--subject needs a value")
			}
			subject = args[i]
		case "--role":
			i++
			if i >= len(args) {
				return "", "", "", fmt.Errorf("--role needs user|admin")
			}
			role = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", "", fmt.Errorf("unknown flag %q", args[i])
			}
			positionals = append(positionals, args[i])
		}
	}
	if ws == "" && len(positionals) > 0 {
		ws = positionals[0]
		positionals = positionals[1:]
	}
	if subject == "" && len(positionals) > 0 {
		subject = positionals[0]
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
				i++
				if i >= len(args) {
					return fmt.Errorf("--key needs a value")
				}
				key = args[i]
			case "--global":
			default:
				if !strings.HasPrefix(args[i], "-") && key == "" {
					key = args[i]
				}
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
