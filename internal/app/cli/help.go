package cli

import (
	"fmt"
	"strings"
)

// wantsHelp reports whether argv asks for help (-h/--help/help).
func wantsHelp(args []string) bool {
	for _, a := range args {
		switch a {
		case "-h", "--help", "help":
			return true
		}
	}
	return false
}

// helpPath strips help flags and returns the command path for nested help.
func helpPath(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "-h", "--help", "help":
			continue
		default:
			out = append(out, a)
		}
	}
	return out
}

func printHelp(args []string) error {
	path := helpPath(args)
	fmt.Print(helpText(path))
	return nil
}

func helpText(path []string) string {
	if len(path) == 0 {
		return rootHelp
	}
	key := strings.Join(path, " ")
	if t, ok := helpTree[key]; ok {
		return t
	}
	for i := len(path) - 1; i >= 1; i-- {
		parent := strings.Join(path[:i], " ")
		if t, ok := helpTree[parent]; ok {
			return t
		}
	}
	if t, ok := helpTree[path[0]]; ok {
		return t
	}
	return rootHelp
}

const rootHelp = `cautem — agent sandbox CLI

Usage:
  cautem [global flags] <command> [flags]

Global flags:
  -g, --gateway NAME     Select gateway (or CAUTEM_GATEWAY)
  --workspace NAME       Default workspace (or CAUTEM_WORKSPACE)
  -o, --output FORMAT    text|json|yaml

Commands:
  sandbox (sb)       Create and manage sandboxes
  cp                 Copy files between the host and a sandbox
  exec               Execute a command in a sandbox
  provider           Provider instances and profiles
  profile            Reusable provider profile definitions
  policy (pol)       Network policy get/set/update
  rule (rl)          Approval rules
  gateway (gw)       Add/select/login gateways
  workspace (ws)     Workspaces and members
  service (svc)      Expose HTTP services through the gateway
  forward (fwd)      TCP port forwards
  inference          Route inference.local
  settings           Gateway/local settings
  logs (lg)          Sandbox logs
  status             Gateway connectivity
  health             Docker/Podman and gateway health probe
  init               Write an agent starter policy
  doctor (dr)        Environment checks
  whoami             Identity (supports -o json)
  term               Interactive TUI
  install            Install CLI + ensure local gateway
  ssh-proxy          SSH ProxyCommand helper used by sandbox ssh-config
  completions        Shell completions
  version            Print version

Run 'cautem <command> --help' for details.
`

var helpTree = map[string]string{
	"sandbox": `cautem sandbox — manage sandboxes

Usage:
  cautem sandbox create|attach|list|get|stop|start|delete|exec|connect|upload|download|ssh-config|provider|template …

Aliases: sb

Examples:
  cautem sandbox create --name app --from ollama
  cautem sandbox list
  cautem sandbox attach app
  cautem sandbox exec app -- ls /
`,
	"sandbox attach": `cautem sandbox attach — reconnect to a running canonical process

Usage:
  cautem sandbox attach NAME [--tty]

Use --tty or -t for a process created with a TTY; stdin, output, terminal resize, and
the canonical process exit status are reconnected to the current terminal.
Without --tty, attach follows canonical stdout/stderr and reports its exit code.
`,
	"sandbox exec": `cautem sandbox exec — execute a command in a running sandbox

Usage:
  cautem sandbox exec [NAME] [flags] -- COMMAND [ARG…]

Flags:
  -n, --name NAME     Select sandbox by name
  --workdir DIR       Working directory inside the sandbox
  --timeout SECONDS   Bound command execution time (0 disables the deadline)
  --tty               Force pseudo-terminal allocation
  --no-tty            Disable pseudo-terminal allocation
  --no-login-shell    Run the command with a non-login shell
  --env KEY=VALUE     Set a non-secret command environment variable (repeatable)

When stdin is piped, cautem forwards it without allocating a pseudo-terminal.
`,
	"sandbox create": `cautem sandbox create — create a sandbox

Usage:
  cautem sandbox create --name NAME [flags]

Flags:
  --name NAME
  --remote --gateway-workspace NAME  Create on the selected Gateway compute backend
  --gateway URL         Select the Gateway endpoint
  --from base|ollama|cursor|claude|IMAGE_REF
  --image IMAGE
  --build DIR           Build a local image from a Docker-compatible context
  --dockerfile PATH     Dockerfile path relative to --build (default Dockerfile)
  --policy PATH
  --cpu CORES --memory SIZE   CPU also accepts millicores (500m)
  --gpu [COUNT] --cdi DEVICE_ID (repeatable)
  --template NAME       Apply saved workload defaults; cannot mix with inline image/resources/env
  --pids-limit N          (-1 unlimited; default 2048 via driver)
  --provider NAME (repeatable)
  --forward PORT (repeatable)
  --workspace PATH
  --label KEY=VALUE
  --env KEY=VALUE (repeatable)
  --driver-config-json JSON
  --restart-policy never|on-failure|always
  --upload PATH [--no-git-ignore]
  --auto-providers | --no-auto-providers
  --no-credential-warnings
  --approval-mode manual|auto
  --editor vscode|cursor
  --display none|novnc [--display-port PORT] [--open-display]
  --no-proxy --no-harden --no-volume --no-host-internal --no-keep --i-know
  --tty                   Allocate a PTY for the canonical process after --
  --detach                Start the canonical process without attaching

Arguments after -- run as the sandbox's canonical main process. A terminal
allocates a PTY automatically; use --tty to force one for redirected input.
With --remote, a foreground command attaches to the Gateway canonical process;
use --detach to return immediately and sandbox attach NAME to reconnect.
`,
	"sandbox template": `cautem sandbox template — workload templates

Usage:
  cautem sandbox template create|list|get|delete …

Create accepts a positional template name and workload fields:
  cautem sandbox template create NAME --image IMAGE --cpu 2 --memory 4Gi --gpu 1
  --env KEY=VALUE --label KEY=VALUE --driver-config-json JSON

Add --gateway to create/list/get/delete a workspace-scoped template on the
selected Gateway. Without it, templates remain local to this CLI profile.
`,
	"sandbox provider": `cautem sandbox provider — attach providers to a sandbox

Usage:
  cautem sandbox provider list|attach|detach …
`,
	"sandbox list": `cautem sandbox list — list sandboxes

Usage:
  cautem sandbox list [--limit N] [--offset N] [--selector EXPR] [-o table|json]
  cautem sandbox list [--ids|--names] [--all-workspaces]

Flags:
  --limit N             Maximum rows (default 100)
  --offset N            Skip rows before output
  --selector EXPR       Filter by sandbox labels
  -o, --output FORMAT   table or json
  --ids                 Print only sandbox IDs
  --names               Print only sandbox names
  --all-workspaces      Include sandboxes from every workspace

The ls alias is also accepted.
`,
	"sandbox get": `cautem sandbox get — inspect one sandbox

Usage:
  cautem sandbox get NAME
`,
	"sandbox start": `cautem sandbox start — start a stopped sandbox

Usage:
  cautem sandbox start [NAME]
`,
	"sandbox stop": `cautem sandbox stop — stop a running sandbox

Usage:
  cautem sandbox stop [NAME]
`,
	"sandbox delete": `cautem sandbox delete — delete one or more sandboxes

Usage:
  cautem sandbox delete NAME...
  cautem sandbox delete --all

The rm and remove aliases are also accepted. --all cannot be combined with names.
`,
	"sandbox upload": `cautem sandbox upload — upload files into a sandbox

Usage:
  cautem sandbox upload NAME LOCAL_PATH [SANDBOX_DEST] [--no-git-ignore]

The default destination is /workspace. --no-git-ignore includes files matched
by .gitignore and is available only for upload.
`,
	"sandbox download": `cautem sandbox download — download files from a sandbox

Usage:
  cautem sandbox download NAME SANDBOX_PATH [LOCAL_DEST]

The default local destination is the current directory.
`,
	"sandbox ssh-config": `cautem sandbox ssh-config — print or install SSH configuration

Usage:
  cautem sandbox ssh-config NAME [--install]
`,
	"sandbox connect": `cautem sandbox connect — connect with an editor or command

Usage:
  cautem sandbox connect [NAME] [--editor vscode|cursor]
  cautem sandbox connect [NAME] -- COMMAND [ARG...]

--editor cannot be combined with a command.
`,
	"sandbox provider list": `cautem sandbox provider list — list providers attached to a sandbox

Usage:
  cautem sandbox provider list SANDBOX
`,
	"sandbox provider attach": `cautem sandbox provider attach — attach a provider

Usage:
  cautem sandbox provider attach SANDBOX PROVIDER
`,
	"sandbox provider detach": `cautem sandbox provider detach — detach a provider

Usage:
  cautem sandbox provider detach SANDBOX PROVIDER
`,
	"sandbox template create": `cautem sandbox template create — save workload defaults

Usage:
  cautem sandbox template create [NAME|--name NAME] [flags]

Flags:
  NAME or --name NAME       Template name
  --gateway                 Store the template in the selected Gateway workspace
  --image IMAGE             Workload image
  --from SOURCE             Preset or image source
  --policy PATH             Network policy file
  --cpu CORES               CPU limit, including millicore values
  --memory SIZE             Memory limit
  --pids-limit N            Process limit (-1 means unlimited)
  --gpu [COUNT]             Optional GPU count
  --env KEY=VALUE           Environment value (repeatable)
  --label KEY=VALUE         Template label (repeatable)
  --driver-config-json JSON Backend-specific configuration
  --provider NAME           Provider to attach (repeatable)
`,
	"sandbox template list": `cautem sandbox template list — list saved templates

Usage:
  cautem sandbox template list [--gateway]
`,
	"sandbox template get": `cautem sandbox template get — inspect a saved template

Usage:
  cautem sandbox template get NAME [--gateway]
`,
	"sandbox template delete": `cautem sandbox template delete — delete a saved template

Usage:
  cautem sandbox template delete NAME [--gateway]
`,
	"provider": `cautem provider — provider instances

Usage:
  cautem provider create|list|get|update|delete|profile|refresh|effective …

Examples:
  cautem provider create --name gh --type github --from-existing
  cautem provider refresh configure NAME --credential-key K --strategy oauth2-refresh-token
`,
	"provider profile": `cautem provider profile — custom provider YAML profiles

Usage:
  cautem profile list|show|import|update|export|delete|lint …
  cautem profile import --url https://example.org/profile.yaml
  cautem profile import --from ./provider-profiles
`,
	"profile": `cautem profile — reusable provider definitions

Usage:
  cautem profile list|show|import|update|export|delete|lint …
`,
	"provider refresh": `cautem provider refresh — credential refresh strategies

Usage:
  cautem provider refresh status|configure|rotate|delete …

Strategies: env | oauth2-refresh-token | oauth2-client-credentials | aws-sts-assume-role
`,
	"policy": `cautem policy — network policy

Usage:
  cautem policy get|set|update|list|delete|check …
`,
	"gateway": `cautem gateway — manage gateways

Usage:
  cautem gateway ensure|add|remove|select|info|list|login|logout

  ensure   start/select local gateway on 127.0.0.1:7443 if needed
`,
	"workspace": `cautem workspace — workspaces (gateway-backed)

Usage:
  cautem workspace create --name NAME
  cautem workspace get NAME
  cautem workspace list [--page-size N] [--page-token TOKEN] [--label-selector EXPR] [-o table|yaml|json]
  cautem workspace delete NAME...
  cautem workspace member add|remove|list …
`,
	"workspace create": `cautem workspace create — create a workspace

Usage:
  cautem workspace create --name NAME [--label KEY=VALUE ...]

Repeat --label to attach workspace metadata labels.
`,
	"workspace delete": `cautem workspace delete — delete one or more workspaces

Usage:
  cautem workspace delete NAME...
`,
	"workspace list": `cautem workspace list — list a page of workspaces

Usage:
  cautem workspace list [--page-size N] [--page-token TOKEN] [--label-selector EXPR] [-o table|yaml|json]

The default page size is 100. The returned page token can be passed to --page-token.
`,
	"workspace member": `cautem workspace member — manage members

Usage:
  cautem workspace member add --workspace NAME --subject SUBJECT [--role user|admin]
  cautem workspace member remove --workspace NAME --subject SUBJECT
  cautem workspace member list --workspace NAME
`,
	"service": `cautem service — expose HTTP services

Usage:
  cautem service expose <sandbox> <port> [name]
  cautem service list|get|delete …

URL: use the service address returned by the expose command
`,
	"forward": `cautem forward — TCP forwards into a sandbox

Usage:
  cautem forward start <host-port> <sandbox> [-d]
  cautem forward stop <id>
  cautem forward list
`,
	"inference": `cautem inference — inference.local routing

Usage:
  cautem inference get|set|update|list|show|local
`,
	"settings": `cautem settings — key/value settings

Usage:
  cautem settings get|set|delete …
`,
	"logs": `cautem logs — sandbox logs

Usage:
  cautem logs [name] [--tail] [-n N] [--since 5m]
  Name defaults to the last-used sandbox.
`,
	"doctor": `cautem doctor — environment checks

Usage:
  cautem doctor check
  cautem doctor cleanup [--dry-run|--yes]

cleanup is a scoped dry-run by default. With --yes it removes only dangling
anonymous Testcontainers volumes and stopped containers labeled cautem=1.
`,
	"install": `cautem install — install CLI binary and ensure local gateway

Usage:
  cautem install [--force]

Copies cautem to ~/.local/share/cautem/bin, symlinks ~/.local/bin/cautem,
then starts/selects a local cautem-gateway if needed.
`,
	"whoami": `cautem whoami — print identity

Usage:
  cautem whoami
  cautem -o json whoami
`,
	"completions": `cautem completions — shell completions

Usage:
  cautem completions <bash|zsh|fish|powershell>
`,
	"ssh-proxy": `cautem ssh-proxy — SSH ProxyCommand helper

Usage:
  cautem ssh-proxy --gateway URL --sandbox-id ID --token TOKEN [--gateway-name NAME]
  cautem ssh-proxy --gateway-name NAME --name SANDBOX
  cautem ssh-proxy --server URL --name SANDBOX
  cautem ssh-proxy SANDBOX

This command is invoked by SSH configurations generated with sandbox ssh-config.
`,
	"version": `cautem version — print the installed CLI version

Usage:
  cautem version
`,
	"status": `cautem status — gateway connectivity

Usage:
  cautem status
  cautem -o json status
`,
	"health": `cautem health — engine and gateway health probe

Usage:
  cautem health
  cautem -o json health
`,
	"init": `cautem init — write an agent starter policy

Usage:
  cautem init --agent cursor [--dir DIR] [--force]
`,
	"exec": `cautem exec — execute a command in a sandbox

Usage:
  cautem exec [--name] NAME -- COMMAND [ARG ...]
`,
	"cp": `cautem cp — copy files between the host and a sandbox

Usage:
  cautem cp <local-path> <sandbox>:/path
  cautem cp <sandbox>:/path <local-path>
`,
	"term": `cautem term — interactive TUI

Usage:
  cautem term
`,
	"rule": `cautem rule — approval rules (MVP)

Usage:
  cautem rule get|approve|approve-all|reject|history|clear …
`,
}
