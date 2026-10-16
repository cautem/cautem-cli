// Package osargs parses OpenShell CLI argv shapes.
// Keep parsers pure (no I/O) so unit tests pin the wire format.
package osargs

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SandboxDelete parses `sandbox delete NAME...` and `sandbox delete --all`.
type SandboxDelete struct {
	Names []string
	All   bool
}

// SandboxAttach parses `sandbox attach NAME [--tty]`.
type SandboxAttach struct {
	Name string
	TTY  bool
}

func ParseSandboxAttach(args []string) (SandboxAttach, error) {
	var out SandboxAttach
	seenTTY := false
	for _, arg := range args {
		switch arg {
		case "--tty", "-t":
			if seenTTY {
				return SandboxAttach{}, fmt.Errorf("sandbox attach: duplicate --tty")
			}
			out.TTY = true
			seenTTY = true
		default:
			if strings.HasPrefix(arg, "-") {
				return SandboxAttach{}, fmt.Errorf("sandbox attach: unknown flag %q", arg)
			}
			if out.Name != "" {
				return SandboxAttach{}, fmt.Errorf("usage: cautem sandbox attach NAME [--tty]")
			}
			out.Name = arg
		}
	}
	if out.Name == "" {
		return SandboxAttach{}, fmt.Errorf("usage: cautem sandbox attach NAME [--tty]")
	}
	return out, nil
}

// ParseSandboxTarget parses commands that accept one optional sandbox name.
func ParseSandboxTarget(args []string, command string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("usage: cautem sandbox %s [name]", command)
	}
	if len(args) == 1 {
		if args[0] == "" || strings.HasPrefix(args[0], "-") {
			return "", fmt.Errorf("sandbox %s: expected a sandbox name", command)
		}
		return args[0], nil
	}
	return "", nil
}

// ParseSandboxDelete accepts one or more sandbox names, or --all exclusively.
func ParseSandboxDelete(args []string) (SandboxDelete, error) {
	var out SandboxDelete
	for _, arg := range args {
		if arg == "--all" {
			if out.All || len(out.Names) > 0 {
				return SandboxDelete{}, fmt.Errorf("sandbox delete: --all conflicts with sandbox names")
			}
			out.All = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return SandboxDelete{}, fmt.Errorf("sandbox delete: unknown flag %q", arg)
		}
		if out.All {
			return SandboxDelete{}, fmt.Errorf("sandbox delete: --all conflicts with sandbox names")
		}
		out.Names = append(out.Names, arg)
	}
	if len(out.Names) == 0 && !out.All {
		return SandboxDelete{}, fmt.Errorf("usage: cautem sandbox delete <name>... | --all")
	}
	return out, nil
}

// GatewayAdd is `openshell gateway add <endpoint> [--name NAME] [--local|--remote USER@HOST]`.
type GatewayAdd struct {
	Endpoint string
	Name     string
	Local    bool
	Remote   string

	OIDCIssuer    string
	OIDCClientID  string
	OIDCAudience  string
	OIDCScopes    string
	OIDCAllowHTTP bool
}

// ParseGatewayAdd accepts OpenShell form:
//
//	gateway add <endpoint> [--name NAME] [--local|--remote USER@HOST]
//	  [--oidc-issuer URL] [--oidc-client-id ID] [--oidc-audience AUD] [--oidc-scopes S]
func ParseGatewayAdd(args []string) (GatewayAdd, error) {
	if len(args) == 0 {
		return GatewayAdd{}, fmt.Errorf("usage: cautem gateway add <endpoint> [--name NAME] [--local] [--oidc-issuer URL]")
	}
	if !looksLikeEndpoint(args[0]) {
		return GatewayAdd{}, fmt.Errorf("usage: cautem gateway add <endpoint> [--name NAME] [--local]")
	}
	out := GatewayAdd{Endpoint: args[0]}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--name":
			i++
			if i >= len(args) {
				return GatewayAdd{}, fmt.Errorf("--name needs a value")
			}
			out.Name = args[i]
		case "--local":
			out.Local = true
		case "--remote":
			i++
			if i >= len(args) {
				return GatewayAdd{}, fmt.Errorf("--remote needs USER@HOST")
			}
			out.Remote = args[i]
		case "--oidc-issuer":
			i++
			if i >= len(args) {
				return GatewayAdd{}, fmt.Errorf("--oidc-issuer needs a value")
			}
			out.OIDCIssuer = args[i]
		case "--oidc-client-id":
			i++
			if i >= len(args) {
				return GatewayAdd{}, fmt.Errorf("--oidc-client-id needs a value")
			}
			out.OIDCClientID = args[i]
		case "--oidc-audience":
			i++
			if i >= len(args) {
				return GatewayAdd{}, fmt.Errorf("--oidc-audience needs a value")
			}
			out.OIDCAudience = args[i]
		case "--oidc-scopes":
			i++
			if i >= len(args) {
				return GatewayAdd{}, fmt.Errorf("--oidc-scopes needs a value")
			}
			out.OIDCScopes = args[i]
		case "--oidc-allow-insecure-http":
			out.OIDCAllowHTTP = true
		default:
			return GatewayAdd{}, fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if out.Name == "" {
		out.Name = deriveGatewayName(out.Endpoint)
	}
	if !out.Local && out.Remote == "" && (strings.HasPrefix(out.Endpoint, "http://127.") || strings.Contains(out.Endpoint, "localhost")) {
		out.Local = true
	}
	return out, nil
}

func looksLikeEndpoint(s string) bool {
	s = strings.TrimSpace(s)
	return strings.Contains(s, "://") || strings.HasPrefix(s, "localhost") || strings.HasPrefix(s, "127.")
}

func deriveGatewayName(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return "gateway"
	}
	host := u.Hostname()
	host = strings.ReplaceAll(host, ".", "-")
	if host == "127-0-0-1" || host == "localhost" {
		return "local"
	}
	return host
}

// PolicySet is `openshell policy set [name] --policy PATH [--wait] [--global] [--yes]`.
type PolicySet struct {
	Name   string
	Path   string
	Wait   bool
	Global bool
	Yes    bool
}

// ParsePolicySet parses OpenShell policy set argv (args after "set").
func ParsePolicySet(args []string) (PolicySet, error) {
	var out PolicySet
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--policy":
			i++
			if i >= len(args) {
				return PolicySet{}, fmt.Errorf("--policy needs a path")
			}
			out.Path = args[i]
		case "--wait":
			out.Wait = true
		case "--global":
			out.Global = true
		case "--yes", "-y":
			out.Yes = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return PolicySet{}, fmt.Errorf("unknown flag %q", args[i])
			}
			if out.Name != "" {
				return PolicySet{}, fmt.Errorf("unexpected argument %q", args[i])
			}
			out.Name = args[i]
		}
	}
	if out.Path == "" {
		return PolicySet{}, fmt.Errorf("usage: cautem policy set [name] --policy <path> [--wait] [--global]")
	}
	if !out.Global && out.Name == "" {
		return PolicySet{}, fmt.Errorf("usage: cautem policy set <name> --policy <path> [--wait]")
	}
	return out, nil
}

// PolicyGet is `openshell policy get [name] [--full|--base] [--rev N] [--global]`.
type PolicyGet struct {
	Name   string
	View   string // full|base
	Rev    int
	Global bool
}

// ParsePolicyGet parses args after "get".
func ParsePolicyGet(args []string) (PolicyGet, error) {
	out := PolicyGet{View: "full"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--full", "--effective":
			out.View = "full"
		case "--base":
			out.View = "base"
		case "--global":
			out.Global = true
		case "--rev":
			i++
			if i >= len(args) {
				return PolicyGet{}, fmt.Errorf("--rev needs a number")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return PolicyGet{}, fmt.Errorf("invalid --rev")
			}
			out.Rev = n
		default:
			if strings.HasPrefix(args[i], "-") {
				return PolicyGet{}, fmt.Errorf("unknown flag %q", args[i])
			}
			if out.Name != "" {
				return PolicyGet{}, fmt.Errorf("unexpected argument %q", args[i])
			}
			out.Name = args[i]
		}
	}
	if !out.Global && out.Name == "" {
		return PolicyGet{}, fmt.Errorf("usage: cautem policy get <name> [--full|--base] [--rev N]")
	}
	return out, nil
}

// ForwardStart is `openshell forward start <port> [name] [-d|--background]`.
type ForwardStart struct {
	Port       string
	Name       string
	Background bool
}

// ParseForwardStart parses args after "start".
func ParseForwardStart(args []string) (ForwardStart, error) {
	if len(args) == 0 {
		return ForwardStart{}, fmt.Errorf("usage: cautem forward start <port> [sandbox] [-d]")
	}
	out := ForwardStart{Port: args[0]}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-d", "--background":
			out.Background = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return ForwardStart{}, fmt.Errorf("unknown flag %q", args[i])
			}
			if out.Name != "" {
				return ForwardStart{}, fmt.Errorf("unexpected argument %q", args[i])
			}
			out.Name = args[i]
		}
	}
	return out, nil
}

// ForwardStop is `openshell forward stop <port> [name]`.
type ForwardStop struct {
	Port string
	Name string
}

// ParseForwardStop parses args after "stop".
func ParseForwardStop(args []string) (ForwardStop, error) {
	if len(args) == 0 {
		return ForwardStop{}, fmt.Errorf("usage: cautem forward stop <port> [sandbox]")
	}
	out := ForwardStop{Port: args[0]}
	if len(args) > 1 {
		out.Name = args[1]
	}
	return out, nil
}

// ServiceExpose is `openshell service expose <sandbox> <target-port> [service]`.
type ServiceExpose struct {
	Sandbox string
	Port    string
	Service string
}

// ParseServiceExpose parses args after "expose".
func ParseServiceExpose(args []string) (ServiceExpose, error) {
	if len(args) < 2 {
		return ServiceExpose{}, fmt.Errorf("usage: cautem service expose <sandbox> <target-port> [service]")
	}
	out := ServiceExpose{Sandbox: args[0], Port: args[1]}
	if len(args) > 2 {
		out.Service = args[2]
	}
	if out.Service == "" {
		out.Service = "default"
	}
	return out, nil
}

// SettingsSet is `openshell settings set [name] --key K --value V [--global]`.
type SettingsSet struct {
	Name   string
	Key    string
	Value  string
	Global bool
}

// ParseSettingsSet parses args after "set".
func ParseSettingsSet(args []string) (SettingsSet, error) {
	var out SettingsSet
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--key":
			i++
			if i >= len(args) {
				return SettingsSet{}, fmt.Errorf("--key needs a value")
			}
			out.Key = args[i]
		case "--value":
			i++
			if i >= len(args) {
				return SettingsSet{}, fmt.Errorf("--value needs a value")
			}
			out.Value = args[i]
		case "--global":
			out.Global = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return SettingsSet{}, fmt.Errorf("unknown flag %q", args[i])
			}
			if out.Name != "" {
				return SettingsSet{}, fmt.Errorf("unexpected argument %q", args[i])
			}
			out.Name = args[i]
		}
	}
	if out.Key == "" || out.Value == "" {
		return SettingsSet{}, fmt.Errorf("usage: cautem settings set [sandbox] --key KEY --value VALUE [--global]")
	}
	return out, nil
}

// SandboxTransfer is upload/download: NAME PATH [DEST].
type SandboxTransfer struct {
	Name        string
	Path        string
	Dest        string
	NoGitIgnore bool
}

// ParseSandboxTransfer parses args after upload|download.
func ParseSandboxTransfer(args []string) (SandboxTransfer, error) {
	var positional []string
	out := SandboxTransfer{}
	for _, arg := range args {
		switch arg {
		case "--no-git-ignore":
			out.NoGitIgnore = true
		default:
			if strings.HasPrefix(arg, "-") {
				return SandboxTransfer{}, fmt.Errorf("unknown sandbox transfer flag %q", arg)
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) < 2 || len(positional) > 3 {
		return SandboxTransfer{}, fmt.Errorf("usage: cautem sandbox upload|download <name> <path> [dest] [--no-git-ignore]")
	}
	out.Name, out.Path = positional[0], positional[1]
	if len(positional) > 2 {
		out.Dest = positional[2]
	}
	return out, nil
}

// LogsOpts mirrors OpenShell logs flags.
type LogsOpts struct {
	Name   string
	N      int
	NSet   bool
	Tail   bool
	All    bool
	Since  string
	Source string
	Level  string
}

// InferenceSet is OpenShell `inference set --provider P --model M`.
type InferenceSet struct {
	Provider   string
	Model      string
	TimeoutSec int
	NoVerify   bool
}

// ParseInferenceSet parses args after inference set|update.
func ParseInferenceSet(args []string) (InferenceSet, error) {
	var out InferenceSet
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider":
			i++
			if i >= len(args) {
				return InferenceSet{}, fmt.Errorf("--provider needs a value")
			}
			out.Provider = args[i]
		case "--model":
			i++
			if i >= len(args) {
				return InferenceSet{}, fmt.Errorf("--model needs a value")
			}
			out.Model = args[i]
		case "--timeout":
			i++
			if i >= len(args) {
				return InferenceSet{}, fmt.Errorf("--timeout needs seconds")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return InferenceSet{}, fmt.Errorf("invalid --timeout")
			}
			out.TimeoutSec = n
		case "--no-verify":
			out.NoVerify = true
		default:
			return InferenceSet{}, fmt.Errorf("unknown flag %q", args[i])
		}
	}
	if out.Provider == "" || out.Model == "" {
		return InferenceSet{}, fmt.Errorf("usage: cautem inference set --provider NAME --model MODEL [--timeout N] [--no-verify]")
	}
	return out, nil
}

// ParseLogs parses args after "logs".
func ParseLogs(args []string) (LogsOpts, error) {
	out := LogsOpts{N: 200, Source: "all"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n":
			i++
			if i >= len(args) {
				return LogsOpts{}, fmt.Errorf("-n needs a number")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 || uint64(n) > uint64(^uint32(0)) {
				return LogsOpts{}, fmt.Errorf("invalid -n")
			}
			out.N = n
			out.NSet = true
		case "--tail", "--follow", "-f":
			out.Tail = true
		case "--all":
			out.All = true
		case "--since":
			i++
			if i >= len(args) {
				return LogsOpts{}, fmt.Errorf("--since needs a duration")
			}
			out.Since = args[i]
		case "--source":
			i++
			if i >= len(args) {
				return LogsOpts{}, fmt.Errorf("--source needs a value")
			}
			out.Source = args[i]
		case "--level":
			i++
			if i >= len(args) {
				return LogsOpts{}, fmt.Errorf("--level needs a value")
			}
			out.Level = args[i]
		case "--name":
			i++
			if i >= len(args) {
				return LogsOpts{}, fmt.Errorf("--name needs a value")
			}
			out.Name = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return LogsOpts{}, fmt.Errorf("unknown flag %q", args[i])
			}
			if out.Name != "" {
				return LogsOpts{}, fmt.Errorf("unexpected argument %q", args[i])
			}
			out.Name = args[i]
		}
	}
	if out.Source != "all" && out.Source != "sandbox" && out.Source != "proxy" {
		return LogsOpts{}, fmt.Errorf("invalid --source %q: expected all, sandbox, or proxy", out.Source)
	}
	if out.Since != "" {
		if duration, err := time.ParseDuration(out.Since); err == nil {
			if duration <= 0 {
				return LogsOpts{}, fmt.Errorf("invalid --since %q: duration must be positive", out.Since)
			}
		} else if _, err := time.Parse(time.RFC3339Nano, out.Since); err != nil {
			return LogsOpts{}, fmt.Errorf("invalid --since %q: expected a duration or RFC3339 timestamp", out.Since)
		}
	}
	return out, nil
}

// SandboxExec is OpenShell `sandbox exec [--name NAME] [--workdir DIR] [--env K=V] -- CMD…`
// or positional `sandbox exec NAME -- CMD…`.
type SandboxExec struct {
	Name         string
	WorkDir      string
	Env          map[string]string
	Argv         []string
	TimeoutSec   uint32
	TTY          bool
	TTYSet       bool
	NoLoginShell bool
}

// ParseSandboxExec parses args after "exec".
func ParseSandboxExec(args []string) (SandboxExec, error) {
	out := SandboxExec{Env: map[string]string{}}
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			out.Argv = append([]string{}, args[i+1:]...)
			break
		}
		switch args[i] {
		case "--name", "-n":
			i++
			if i >= len(args) {
				return SandboxExec{}, fmt.Errorf("--name needs a value")
			}
			out.Name = args[i]
		case "--workdir":
			i++
			if i >= len(args) {
				return SandboxExec{}, fmt.Errorf("--workdir needs a path")
			}
			out.WorkDir = args[i]
		case "--env":
			i++
			if i >= len(args) {
				return SandboxExec{}, fmt.Errorf("--env needs KEY=VALUE")
			}
			k, v, ok := strings.Cut(args[i], "=")
			if !ok || k == "" {
				return SandboxExec{}, fmt.Errorf("--env needs KEY=VALUE")
			}
			out.Env[k] = v
		case "--timeout":
			i++
			if i >= len(args) {
				return SandboxExec{}, fmt.Errorf("--timeout needs seconds (0 means no timeout)")
			}
			seconds, err := strconv.ParseUint(args[i], 10, 32)
			if err != nil {
				return SandboxExec{}, fmt.Errorf("invalid --timeout %q: expected seconds (0 means no timeout)", args[i])
			}
			out.TimeoutSec = uint32(seconds)
		case "--tty", "--no-tty":
			out.TTYSet = true
			out.TTY = args[i] == "--tty"
		case "--no-login-shell":
			out.NoLoginShell = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return SandboxExec{}, fmt.Errorf("unknown flag %q", args[i])
			}
			if out.Name != "" {
				out.Argv = append([]string{}, args[i:]...)
				i = len(args)
				break
			}
			out.Name = args[i]
		}
	}
	if len(out.Argv) == 0 {
		return SandboxExec{}, fmt.Errorf("usage: cautem sandbox exec [name] [--workdir DIR] [--env K=V] -- CMD")
	}
	return out, nil
}
