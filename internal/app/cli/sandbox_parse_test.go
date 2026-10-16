package cli

import (
	"regexp"
	"strings"
	"testing"
)

func TestParseSandboxCreateAcceptsEveryPublicFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"name", []string{"--name", "demo"}},
		{"remote", []string{"--remote"}},
		{"gateway workspace", []string{"--gateway-workspace", "team-a"}},
		{"gateway endpoint", []string{"--gateway", "https://gateway.example.test"}},
		{"source alias", []string{"--from", "base"}},
		{"image", []string{"--image", "alpine:3.20"}},
		{"build context", []string{"--build", "."}},
		{"dockerfile", []string{"--dockerfile", "container/Dockerfile"}},
		{"workspace", []string{"--workspace", "/tmp/work"}},
		{"policy", []string{"--policy", "policy.yaml"}},
		{"operator acknowledgement", []string{"--i-know"}},
		{"no proxy", []string{"--no-proxy"}},
		{"no hardening", []string{"--no-harden"}},
		{"display", []string{"--display", "novnc"}},
		{"display port", []string{"--display-port", "5901"}},
		{"open display", []string{"--open-display"}},
		{"label", []string{"--label", "team=runtime"}},
		{"cpu", []string{"--cpu", "500m"}},
		{"memory", []string{"--memory", "1Gi"}},
		{"gpu default count", []string{"--gpu"}},
		{"gpu count", []string{"--gpu", "2"}},
		{"CDI device", []string{"--cdi", "nvidia.com/gpu=all"}},
		{"template", []string{"--template", "python"}},
		{"pids limit", []string{"--pids-limit", "-1"}},
		{"provider", []string{"--provider", "github"}},
		{"forward", []string{"--forward", "8080"}},
		{"environment", []string{"--env", "MODE=test"}},
		{"restart policy", []string{"--restart-policy", "on-failure"}},
		{"driver config", []string{"--driver-config-json", `{"socket_path":"/run/docker.sock"}`}},
		{"upload", []string{"--upload", "./src"}},
		{"no git ignore", []string{"--upload", "./src", "--no-git-ignore"}},
		{"automatic providers", []string{"--auto-providers"}},
		{"disable automatic providers", []string{"--no-auto-providers"}},
		{"suppress credential warnings", []string{"--no-credential-warnings"}},
		{"detach", []string{"--detach"}},
		{"editor", []string{"--editor", "cursor"}},
		{"no volume", []string{"--no-volume"}},
		{"no host internal", []string{"--no-host-internal"}},
		{"approval mode", []string{"--approval-mode", "auto"}},
		{"no keep", []string{"--no-keep"}},
		{"force tty", []string{"--tty"}},
		{"canonical command", []string{"--", "python", "-c", "print('ready')"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseSandboxCreate(test.args); err != nil {
				t.Fatalf("parseSandboxCreate(%q): %v", test.args, err)
			}
		})
	}

	documentedFlags := map[string]bool{}
	for _, flag := range regexp.MustCompile(`--[a-z0-9-]+`).FindAllString(helpText([]string{"sandbox", "create"}), -1) {
		documentedFlags[flag] = true
	}
	fixtureFlags := map[string]bool{}
	for _, test := range tests {
		for _, arg := range test.args {
			if arg == "--" {
				break
			}
			if strings.HasPrefix(arg, "--") {
				fixtureFlags[arg] = true
			}
		}
	}
	for flag := range documentedFlags {
		if !fixtureFlags[flag] {
			t.Errorf("documented sandbox create flag %q has no parser fixture", flag)
		}
	}
	for flag := range fixtureFlags {
		if !documentedFlags[flag] {
			t.Errorf("sandbox create parser fixture %q is absent from help", flag)
		}
	}
}

func TestParseSandboxCreatePreservesPublicFlagValues(t *testing.T) {
	parsed, err := parseSandboxCreate([]string{
		"--name", "demo",
		"--remote", "--gateway-workspace", "team-a", "--gateway", "https://gateway.example.test",
		"--from", "base", "--image", "alpine:3.20", "--build", ".", "--dockerfile", "Dockerfile",
		"--workspace", "/tmp/work", "--policy", "policy.yaml", "--i-know", "--no-proxy", "--no-harden",
		"--display", "novnc", "--display-port", "5901", "--open-display", "--label", "team=runtime",
		"--cpu", "500m", "--memory", "1Gi", "--gpu", "2", "--cdi", "nvidia.com/gpu=all",
		"--template", "python", "--pids-limit", "-1", "--provider", "github", "--provider", "openai",
		"--forward", "8080", "--forward", "9090", "--env", "MODE=test", "--env", "REGION=local",
		"--restart-policy", "on-failure", "--driver-config-json", `{"socket_path":"/run/docker.sock"}`,
		"--upload", "./src", "--no-git-ignore", "--auto-providers", "--no-auto-providers",
		"--no-credential-warnings", "--detach", "--editor", "cursor", "--no-volume", "--no-host-internal",
		"--approval-mode", "auto", "--no-keep", "--tty", "--", "python", "-c", "print('ready')",
	})
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Name != "demo" || !parsed.Remote || parsed.GatewayWorkspace != "team-a" || parsed.GatewayURL != "https://gateway.example.test" {
		t.Fatalf("gateway selection lost: %+v", parsed)
	}
	if parsed.From != "base" || parsed.Image != "alpine:3.20" || parsed.BuildContext != "." || parsed.Dockerfile != "Dockerfile" {
		t.Fatalf("image selection lost: %+v", parsed)
	}
	if parsed.Workspace != "/tmp/work" || !parsed.WorkspaceFlagSet || parsed.Policy != "policy.yaml" {
		t.Fatalf("workspace or policy lost: %+v", parsed)
	}
	if !parsed.IKnow || !parsed.NoProxy || !parsed.NoHarden || parsed.Display != "novnc" || parsed.DisplayPort != 5901 || !parsed.OpenDisplay {
		t.Fatalf("operator or display options lost: %+v", parsed)
	}
	if parsed.Labels["team"] != "runtime" || parsed.CPU != 0.5 || parsed.Memory != "1Gi" || !parsed.GPU || parsed.GPUCount != 2 {
		t.Fatalf("labels or resources lost: %+v", parsed)
	}
	if len(parsed.CDIDevices) != 1 || parsed.CDIDevices[0] != "nvidia.com/gpu=all" || parsed.Template != "python" || parsed.PidsLimit != -1 {
		t.Fatalf("GPU or template settings lost: %+v", parsed)
	}
	if len(parsed.Providers) != 2 || parsed.Providers[0] != "github" || parsed.Providers[1] != "openai" {
		t.Fatalf("provider list lost: %+v", parsed.Providers)
	}
	if len(parsed.Forwards) != 2 || parsed.Forwards[0] != 8080 || parsed.Forwards[1] != 9090 {
		t.Fatalf("forwards lost: %+v", parsed.Forwards)
	}
	if parsed.Env["MODE"] != "test" || parsed.Env["REGION"] != "local" || parsed.RestartPolicy != "on-failure" {
		t.Fatalf("environment or restart policy lost: %+v", parsed)
	}
	if parsed.DriverConfigJSON != `{"socket_path":"/run/docker.sock"}` || parsed.Upload != "./src" || !parsed.NoGitIgnore {
		t.Fatalf("driver config or upload options lost: %+v", parsed)
	}
	if !parsed.AutoProviders || !parsed.NoAutoProviders || !parsed.NoCredentialWarnings || !parsed.Detach || parsed.Editor != "cursor" {
		t.Fatalf("provider/editor options lost: %+v", parsed)
	}
	if !parsed.NoVolume || !parsed.NoHostInternal || parsed.ApprovalMode != "auto" || !parsed.NoKeep || !parsed.ForceTTY {
		t.Fatalf("runtime toggles lost: %+v", parsed)
	}
	if strings.Join(parsed.Argv, " ") != "python -c print('ready')" {
		t.Fatalf("canonical argv=%q", parsed.Argv)
	}
}

func TestParseSandboxCreateRejectsMalformedAndRemovedFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown", []string{"--unknown"}, "unknown flag"},
		{"removed ssh", []string{"--ssh"}, "--ssh was removed"},
		{"missing name", []string{"--name"}, "--name needs a value"},
		{"empty build context", []string{"--build", " "}, "--build needs a local context directory"},
		{"invalid display port", []string{"--display-port", "0"}, "invalid --display-port"},
		{"display port with trailing text", []string{"--display-port", "5901xyz"}, "invalid --display-port"},
		{"display port above range", []string{"--display-port", "65536"}, "invalid --display-port"},
		{"invalid label", []string{"--label", "missing-value"}, "--label needs key=value"},
		{"invalid environment", []string{"--env", "missing-value"}, "--env needs KEY=VALUE"},
		{"invalid CPU", []string{"--cpu", "not-a-cpu"}, "invalid CPU quantity"},
		{"invalid forward", []string{"--forward", "nope"}, "invalid --forward port"},
		{"forward port with trailing text", []string{"--forward", "8080xyz"}, "invalid --forward port"},
		{"forward port above range", []string{"--forward", "65536"}, "invalid --forward port"},
		{"negative forward port", []string{"--forward", "-1"}, "invalid --forward port"},
		{"zero PIDs", []string{"--pids-limit", "0"}, "invalid --pids-limit"},
		{"invalid restart policy", []string{"--restart-policy", "sometimes"}, "invalid --restart-policy"},
		{"zero GPU", []string{"--gpu", "0"}, "invalid --gpu count"},
		{"no git ignore without upload", []string{"--no-git-ignore"}, "requires --upload"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseSandboxCreate(test.args)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseSandboxCreate(%q) error=%v; want substring %q", test.args, err, test.want)
			}
		})
	}
}

func TestParseSandboxCreateRejectsMissingRequiredValues(t *testing.T) {
	flags := []string{
		"--name", "--gateway-workspace", "--gateway", "--from", "--image", "--build", "--dockerfile",
		"--workspace", "--policy", "--display", "--display-port", "--label", "--cdi", "--provider",
		"--env", "--editor", "--forward", "--upload", "--cpu", "--memory", "--pids-limit", "--template",
		"--restart-policy", "--driver-config-json", "--approval-mode",
	}
	for _, flag := range flags {
		for _, next := range []string{"--remote", "--"} {
			t.Run(flag+" before "+next, func(t *testing.T) {
				if _, err := parseSandboxCreate([]string{flag, next}); err == nil {
					t.Fatalf("parseSandboxCreate(%q) accepted a missing value", []string{flag, next})
				}
			})
		}
	}
}

func TestParseSandboxCreateAcceptsPortBoundaries(t *testing.T) {
	parsed, err := parseSandboxCreate([]string{"--display-port", "1", "--forward", "65535"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.DisplayPort != 1 || len(parsed.Forwards) != 1 || parsed.Forwards[0] != 65535 {
		t.Fatalf("valid TCP port boundaries were not preserved: %+v", parsed)
	}
}
