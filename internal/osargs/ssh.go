package osargs

import (
	"fmt"
	"strings"
)

// SSHProxy is OpenShell `ssh-proxy` in its three shapes:
//
//	token:  ssh-proxy --gateway URL --sandbox-id ID --token TOK [--gateway-name G]
//	name:   ssh-proxy --gateway-name G --name N [--server URL]   (-g alias)
//	legacy: ssh-proxy --server URL --name N
//
// plus the cautem shorthand `ssh-proxy <sandbox>` (current gateway).
type SSHProxy struct {
	GatewayURL  string
	GatewayName string
	SandboxID   string
	Token       string
	Name        string
}

// ParseSSHProxy parses args after "ssh-proxy".
func ParseSSHProxy(args []string) (SSHProxy, error) {
	var out SSHProxy
	var server string
	for i := 0; i < len(args); i++ {
		a := args[i]
		key, val, hasEq := strings.Cut(a, "=")
		if !strings.HasPrefix(a, "-") {
			if out.Name != "" {
				return SSHProxy{}, fmt.Errorf("ssh-proxy: unexpected argument %q", a)
			}
			out.Name = a
			continue
		}
		if !hasEq {
			key = a
			if i+1 >= len(args) {
				return SSHProxy{}, fmt.Errorf("ssh-proxy: %s needs a value", a)
			}
			i++
			val = args[i]
		}
		switch key {
		case "--gateway":
			out.GatewayURL = val
		case "--server":
			server = val
		case "--gateway-name", "-g":
			out.GatewayName = val
		case "--sandbox-id":
			out.SandboxID = val
		case "--token":
			out.Token = val
		case "--name":
			out.Name = val
		default:
			return SSHProxy{}, fmt.Errorf("ssh-proxy: unknown flag %q", key)
		}
	}
	if out.GatewayURL == "" {
		out.GatewayURL = server
	}
	switch {
	case out.Token != "":
		if out.SandboxID == "" && out.Name == "" {
			return SSHProxy{}, fmt.Errorf("ssh-proxy: --token requires --sandbox-id")
		}
		if out.GatewayURL == "" && out.GatewayName == "" {
			return SSHProxy{}, fmt.Errorf("ssh-proxy: --token requires --gateway URL or --gateway-name")
		}
	case out.Name == "":
		return SSHProxy{}, fmt.Errorf("usage: cautem ssh-proxy --gateway-name G --name SANDBOX | --gateway URL --sandbox-id ID --token TOK | <sandbox>")
	}
	return out, nil
}

// SandboxConnect is `sandbox connect [name] [--editor vscode|cursor] [-- cmd...]`.
type SandboxConnect struct {
	Name   string
	Editor string
	Argv   []string
}

// ParseSandboxConnect parses args after "connect".
func ParseSandboxConnect(args []string) (SandboxConnect, error) {
	var out SandboxConnect
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			out.Argv = append([]string{}, args[i+1:]...)
			i = len(args)
		case a == "--editor":
			if i+1 >= len(args) {
				return SandboxConnect{}, fmt.Errorf("--editor needs vscode|cursor")
			}
			i++
			out.Editor = args[i]
		case strings.HasPrefix(a, "--editor="):
			out.Editor = strings.TrimPrefix(a, "--editor=")
		case strings.HasPrefix(a, "-"):
			return SandboxConnect{}, fmt.Errorf("connect: unknown flag %q", a)
		default:
			if out.Name != "" {
				return SandboxConnect{}, fmt.Errorf("connect: unexpected argument %q (use -- before a command)", a)
			}
			out.Name = a
		}
	}
	if out.Editor != "" {
		out.Editor = strings.ToLower(out.Editor)
		if out.Editor != "vscode" && out.Editor != "cursor" {
			return SandboxConnect{}, fmt.Errorf("--editor must be vscode|cursor, got %q", out.Editor)
		}
		if len(out.Argv) > 0 {
			return SandboxConnect{}, fmt.Errorf("connect: --editor cannot be combined with a command")
		}
	}
	return out, nil
}

// SandboxSSHConfig is `sandbox ssh-config <name> [--install]`.
type SandboxSSHConfig struct {
	Name    string
	Install bool
}

// ParseSandboxSSHConfig parses args after "ssh-config".
func ParseSandboxSSHConfig(args []string) (SandboxSSHConfig, error) {
	var out SandboxSSHConfig
	for _, a := range args {
		switch {
		case a == "--install":
			out.Install = true
		case strings.HasPrefix(a, "-"):
			return SandboxSSHConfig{}, fmt.Errorf("ssh-config: unknown flag %q", a)
		case out.Name == "":
			out.Name = a
		default:
			return SandboxSSHConfig{}, fmt.Errorf("ssh-config: unexpected argument %q", a)
		}
	}
	if out.Name == "" {
		return SandboxSSHConfig{}, fmt.Errorf("usage: cautem sandbox ssh-config <name> [--install]")
	}
	return out, nil
}
