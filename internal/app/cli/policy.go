package cli

import (
	"fmt"
	"strings"

	"github.com/cautem/cautem-cli/internal/osargs"
	"github.com/cautem/cautem-cli/internal/service"
)

func runPolicy(a *service.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cautem policy get|set|update|list|delete|check …")
	}
	switch args[0] {
	case "check": // cautem-only validation helper
		path := "policies/default.yaml"
		if len(args) > 1 {
			path = args[1]
		}
		return a.PolicyCheck(path)
	case "get":
		parsed, err := osargs.ParsePolicyGet(args[1:])
		if err != nil {
			return err
		}
		if parsed.Global {
			return a.PolicyGlobalGet()
		}
		if parsed.Rev > 0 {
			return a.PolicyGetRevision(parsed.Name, parsed.Rev)
		}
		return a.PolicyGet(parsed.Name, parsed.View)
	case "set":
		parsed, err := osargs.ParsePolicySet(args[1:])
		if err != nil {
			return err
		}
		if parsed.Global {
			return a.PolicyGlobalSet(parsed.Path)
		}
		return a.PolicySet(parsed.Name, parsed.Path, parsed.Wait)
	case "update":
		return runPolicyUpdate(a, args[1:])
	case "list", "ls":
		global := false
		name := ""
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--global":
				global = true
			default:
				if strings.HasPrefix(args[i], "-") {
					return fmt.Errorf("unknown flag %q", args[i])
				}
				name = args[i]
			}
		}
		if global {
			fmt.Println("policy list --global: (use policy get --global for current YAML)")
			return a.PolicyGlobalGet()
		}
		if name == "" {
			return fmt.Errorf("usage: cautem policy list <sandbox>")
		}
		return a.PolicyList(name)
	case "delete", "rm":
		global := false
		for _, f := range args[1:] {
			if f == "--global" {
				global = true
			}
		}
		if !global {
			return fmt.Errorf("usage: cautem policy delete --global")
		}
		fmt.Println("policy delete --global: clearing gateway global policy")
		return a.PolicyGlobalClear()
	default:
		return fmt.Errorf("unknown policy subcommand %q", args[0])
	}
}

func runPolicyUpdate(a *service.App, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: cautem policy update <sandbox> --add-endpoint SPEC [--add-allow SPEC] [--add-deny SPEC] [--binary PATH] [--wait]")
	}
	name := args[0]
	wait := false
	var endpoints, allows, denies, binaries []string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--add-endpoint":
			i++
			if i >= len(args) {
				return fmt.Errorf("--add-endpoint needs a value")
			}
			endpoints = append(endpoints, args[i])
		case "--add-allow":
			i++
			if i >= len(args) {
				return fmt.Errorf("--add-allow needs a value")
			}
			allows = append(allows, args[i])
		case "--add-deny":
			i++
			if i >= len(args) {
				return fmt.Errorf("--add-deny needs a value")
			}
			denies = append(denies, args[i])
		case "--binary":
			i++
			if i >= len(args) {
				return fmt.Errorf("--binary needs a path")
			}
			binaries = append(binaries, args[i])
		case "--wait":
			wait = true
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return a.PolicyUpdate(name, endpoints, allows, denies, binaries, wait)
}
