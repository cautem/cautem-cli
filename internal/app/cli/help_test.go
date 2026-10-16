package cli

import (
	"strings"
	"testing"

	"github.com/cautem/quarshell-cli/internal/service"
)

func TestHelpNested(t *testing.T) {
	root := helpText(nil)
	if root == "" || !strings.Contains(root, "cautem") {
		t.Fatal("root help empty")
	}
	if strings.Contains(root, "OpenShell") || strings.Contains(root, "OPENSHELL_") {
		t.Fatalf("root help exposes compatibility branding or legacy environment names: %s", root)
	}
	serviceHelp := helpText([]string{"service"})
	if strings.Contains(serviceHelp, "openshell.localhost") {
		t.Fatalf("service help exposes a compatibility hostname: %s", serviceHelp)
	}
	sb := helpText([]string{"sandbox"})
	if !strings.Contains(sb, "sandbox create") {
		t.Fatalf("sandbox help: %s", sb)
	}
	attach := helpText([]string{"sandbox", "attach"})
	if !strings.Contains(attach, "sandbox attach NAME [--tty]") {
		t.Fatalf("attach help: %s", attach)
	}
	create := helpText([]string{"sandbox", "create"})
	if !strings.Contains(create, "--name") {
		t.Fatalf("create help: %s", create)
	}
	exec := helpText([]string{"sandbox", "exec"})
	for _, flag := range []string{"--workdir", "--timeout", "--tty", "--no-tty", "--no-login-shell", "--env"} {
		if !strings.Contains(exec, flag) {
			t.Errorf("sandbox exec help missing %s: %s", flag, exec)
		}
	}
	if !wantsHelp([]string{"sandbox", "--help"}) {
		t.Fatal("wantsHelp")
	}
	doctor := helpText([]string{"doctor"})
	if !strings.Contains(doctor, "--dry-run|--yes") {
		t.Fatalf("doctor help: %s", doctor)
	}
	if err := runDoctor(nil, []string{"cleanup", "--yes", "--dry-run"}); err == nil {
		t.Fatal("doctor cleanup accepted conflicting confirmation flags")
	}
	path := helpPath([]string{"service", "expose", "-h"})
	if len(path) != 2 || path[0] != "service" {
		t.Fatalf("helpPath %#v", path)
	}
}

func TestPublicTopLevelCommandsHaveSpecificHelp(t *testing.T) {
	root := helpText(nil)
	commands := []string{
		"version", "health", "init", "status", "doctor", "policy", "sandbox", "exec", "cp", "term",
		"gateway", "logs", "provider", "profile", "whoami", "workspace", "settings", "forward", "service",
		"rule", "inference", "completions", "ssh-proxy", "install",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			if !strings.Contains(root, "\n  "+command+" ") {
				t.Fatalf("root help does not list public command %q", command)
			}
			help := helpText([]string{command})
			if help == root || !strings.Contains(help, "Usage:") {
				t.Fatalf("command %q has no specific usage: %s", command, help)
			}
		})
	}
}

func TestPublicSandboxLeavesHaveSpecificHelp(t *testing.T) {
	tests := []struct {
		path []string
		want []string
	}{
		{path: []string{"sandbox", "attach"}, want: []string{"NAME", "--tty", "-t"}},
		{path: []string{"sandbox", "create"}, want: []string{
			"--name", "--remote", "--gateway-workspace", "--gateway", "--from", "--image", "--build", "--dockerfile",
			"--policy", "--i-know", "--no-proxy", "--no-harden", "--display", "--display-port", "--open-display",
			"--label", "--gpu", "--cdi", "--no-volume", "--no-host-internal", "--provider", "--auto-providers",
			"--no-auto-providers", "--no-credential-warnings", "--env", "--detach", "--editor", "--forward", "--upload",
			"--no-git-ignore", "--cpu", "--memory", "--pids-limit", "--template", "--restart-policy", "--driver-config-json",
			"--approval-mode", "--no-keep", "--tty",
		}},
		{path: []string{"sandbox", "exec"}, want: []string{"-n", "--name", "--workdir", "--timeout", "--tty", "--no-tty", "--no-login-shell", "--env"}},
		{path: []string{"sandbox", "list"}, want: []string{"--limit", "--offset", "--selector", "-o", "--output", "--ids", "--names", "--all-workspaces"}},
		{path: []string{"sandbox", "get"}, want: []string{"cautem sandbox get NAME"}},
		{path: []string{"sandbox", "start"}, want: []string{"cautem sandbox start [NAME]"}},
		{path: []string{"sandbox", "stop"}, want: []string{"cautem sandbox stop [NAME]"}},
		{path: []string{"sandbox", "delete"}, want: []string{"NAME...", "--all", "rm", "remove"}},
		{path: []string{"sandbox", "upload"}, want: []string{"LOCAL_PATH", "SANDBOX_DEST", "--no-git-ignore"}},
		{path: []string{"sandbox", "download"}, want: []string{"SANDBOX_PATH", "LOCAL_DEST"}},
		{path: []string{"sandbox", "ssh-config"}, want: []string{"--install"}},
		{path: []string{"sandbox", "connect"}, want: []string{"--editor vscode|cursor", "-- COMMAND"}},
		{path: []string{"sandbox", "provider", "list"}, want: []string{"SANDBOX"}},
		{path: []string{"sandbox", "provider", "attach"}, want: []string{"SANDBOX PROVIDER"}},
		{path: []string{"sandbox", "provider", "detach"}, want: []string{"SANDBOX PROVIDER"}},
		{path: []string{"sandbox", "template", "create"}, want: []string{
			"NAME|--name NAME", "--gateway", "--name", "--image", "--from", "--policy", "--cpu", "--memory", "--pids-limit",
			"--gpu", "--env", "--label", "--driver-config-json", "--provider",
		}},
		{path: []string{"sandbox", "template", "list"}, want: []string{"--gateway"}},
		{path: []string{"sandbox", "template", "get"}, want: []string{"NAME", "--gateway"}},
		{path: []string{"sandbox", "template", "delete"}, want: []string{"NAME", "--gateway"}},
	}
	for _, test := range tests {
		name := strings.Join(test.path, " ")
		t.Run(name, func(t *testing.T) {
			help := helpText(test.path)
			if help == helpText(nil) || !strings.Contains(help, "Usage:") {
				t.Fatalf("%s has no leaf-specific help: %s", name, help)
			}
			for _, want := range test.want {
				if !strings.Contains(help, want) {
					t.Errorf("%s help is missing %q: %s", name, want, help)
				}
			}
		})
	}
}

func TestWorkspaceListHelpDocumentsPaginationAndFormats(t *testing.T) {
	help := helpText([]string{"workspace", "list"})
	for _, want := range []string{"--page-size", "--page-token", "--label-selector", "table|yaml|json"} {
		if !strings.Contains(help, want) {
			t.Errorf("workspace list help is missing %q: %s", want, help)
		}
	}
}

func TestEveryDocumentedCommandPathHasLeafSpecificHelp(t *testing.T) {
	for commandPath := range helpTree {
		commandPath := commandPath
		t.Run(commandPath, func(t *testing.T) {
			path := strings.Fields(commandPath)
			help := helpText(path)
			if help == helpText(nil) {
				t.Fatalf("documented command path %q fell back to root help", commandPath)
			}
			if !strings.Contains(help, "Usage:") {
				t.Fatalf("documented command path %q has no usage line: %s", commandPath, help)
			}
			if strings.Contains(help, "OpenShell") || strings.Contains(help, "OPENSHELL_") {
				t.Fatalf("product help for %q exposes compatibility branding: %s", commandPath, help)
			}
		})
	}
}

func TestSSHProxyHelpDocumentsSupportedInvocationShapes(t *testing.T) {
	help := helpText([]string{"ssh-proxy"})
	for _, invocation := range []string{
		"--gateway URL --sandbox-id ID --token TOKEN",
		"--gateway-name NAME --name SANDBOX",
		"--server URL --name SANDBOX",
		"cautem ssh-proxy SANDBOX",
	} {
		if !strings.Contains(help, invocation) {
			t.Errorf("ssh-proxy help is missing %q: %s", invocation, help)
		}
	}
}

func TestSandboxAttachRoutesAndValidatesArguments(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := runSandbox(&service.App{}, []string{"attach", "demo", "--unknown"}); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("sandbox attach parser error=%v, want unknown flag", err)
	}
	if err := runSandbox(&service.App{}, []string{"attach", "demo"}); err == nil || !strings.Contains(err.Error(), "compute driver is not configured") {
		t.Fatalf("sandbox attach route error=%v, want service call", err)
	}
}

func TestSandboxCreateRejectsRemovedNoOpSSHFlag(t *testing.T) {
	err := runSandbox(&service.App{}, []string{"create", "--ssh"})
	if err == nil || !strings.Contains(err.Error(), "--ssh was removed") {
		t.Fatalf("error=%v, want explicit removed-flag error", err)
	}
}

func TestCompletionsUsecautemFunctionName(t *testing.T) {
	for _, script := range []string{completionsBash, completionsZsh} {
		if strings.Contains(script, "_osg") || !strings.Contains(script, "_cautem") {
			t.Fatalf("completion function name is stale: %q", script)
		}
	}
}

func TestCompletionScriptsIncludeRootCommands(t *testing.T) {
	commands := []string{"version", "sandbox", "cp", "exec", "provider", "profile", "policy", "gateway", "logs", "term", "status", "health", "init", "doctor", "whoami", "workspace", "forward", "service", "settings", "inference", "rule", "install", "completions", "ssh-proxy"}
	for name, script := range map[string]string{
		"bash":       completionsBash,
		"zsh":        completionsZsh,
		"fish":       completionsFish,
		"powershell": completionsPowerShell,
	} {
		for _, command := range commands {
			if !strings.Contains(script, command) {
				t.Errorf("%s completions do not include %q", name, command)
			}
		}
	}
}
