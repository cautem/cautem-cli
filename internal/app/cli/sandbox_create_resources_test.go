package cli

import (
	"strings"
	"testing"

	"github.com/cautem/quarshell-cli/internal/service"
	"github.com/cautem/quarshell-cli/internal/storage/templates"
)

func TestOptionalGPUCountParser(t *testing.T) {
	for _, test := range []struct {
		args []string
		want int
	}{
		{args: []string{"--gpu", "--from", "base"}, want: 1},
		{args: []string{"--gpu", "2", "--from", "base"}, want: 2},
	} {
		got, _, err := parseOptionalGPUCount(test.args, 0)
		if err != nil || got != test.want {
			t.Errorf("parseOptionalGPUCount(%q)=(%d,%v), want %d", test.args, got, err, test.want)
		}
	}
	for _, count := range []string{"0", "-1", "abc", "4294967296"} {
		if _, _, err := parseOptionalGPUCount([]string{"--gpu", count}, 0); err == nil {
			t.Errorf("--gpu %s unexpectedly succeeded", count)
		}
	}
}

func TestParseTemplateTargetGatewayScope(t *testing.T) {
	name, gateway, err := parseTemplateTarget([]string{"agent", "--gateway"})
	if err != nil || name != "agent" || !gateway {
		t.Fatalf("parse template target = %q, %v, %v", name, gateway, err)
	}
	if _, _, err := parseTemplateTarget([]string{"agent", "other"}); err == nil {
		t.Fatal("accepted multiple template names")
	}
	if _, _, err := parseTemplateTarget([]string{"--unknown"}); err == nil {
		t.Fatal("accepted unknown template target flag")
	}
}

func TestSandboxCreateRemoteFlagsRouteToGatewayPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := runSandbox(&service.App{}, []string{
		"create", "--remote", "--gateway-workspace", "team", "--name", "agent", "--image", "ubuntu:24.04",
		"--cpu", "500m", "--memory", "2Gi", "--gpu", "2", "--cdi", "nvidia.com/gpu=0", "--cdi", "nvidia.com/gpu=1", "--pids-limit", "512", "--env", "FEATURE_FLAG=on",
		"--driver-config-json", `{"docker":{"mounts":[{"type":"tmpfs","target":"/scratch"}]}}`,
	})
	if err == nil || !strings.Contains(err.Error(), "no current gateway") || strings.Contains(err.Error(), "docker not available") {
		t.Fatalf("remote create routing error=%v", err)
	}
}

func TestSandboxTemplateCreateAcceptsCompleteWorkloadShape(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	err := runSandbox(&service.App{}, []string{
		"template", "create", "gpu-kata", "--image", "registry.example.com/team/agent:1.0",
		"--cpu", "500m", "--memory", "4Gi", "--gpu", "2", "--env", "FEATURE_FLAG=on",
		"--label", "team=runtime", "--driver-config-json", `{"docker":{}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := templates.Get("gpu-kata")
	if err != nil {
		t.Fatal(err)
	}
	if got.Image != "registry.example.com/team/agent:1.0" || got.CPU != 0.5 || got.Memory != "4Gi" || got.GPUCount != 2 || got.Env["FEATURE_FLAG"] != "on" || got.Labels["team"] != "runtime" || got.DriverConfigJSON != `{"docker":{}}` {
		t.Fatalf("template workload = %+v", got)
	}
}

func TestSandboxCreateRejectsTemplateAndInlineWorkloadConflict(t *testing.T) {
	for _, args := range [][]string{
		{"create", "--template", "gpu-kata", "--from", "base"},
		{"create", "--template", "gpu-kata", "--cpu", "500m"},
		{"create", "--template", "gpu-kata", "--gpu", "2"},
		{"create", "--template", "gpu-kata", "--env", "MODE=test"},
	} {
		err := runSandbox(&service.App{}, args)
		if err == nil || !strings.Contains(err.Error(), "--template cannot be combined") {
			t.Errorf("runSandbox(%q) error=%v", args, err)
		}
	}
}

func TestSandboxCreateParsesAndValidatesLocalBuildFlags(t *testing.T) {
	if err := runSandbox(&service.App{}, []string{"create", "--dockerfile", "containers/Dockerfile"}); err == nil || !strings.Contains(err.Error(), "--dockerfile requires --build") {
		t.Fatalf("dockerfile without build error=%v", err)
	}
	if err := runSandbox(&service.App{}, []string{"create", "--build", ".", "--image", "alpine"}); err == nil || !strings.Contains(err.Error(), "either --build or --image/--from") {
		t.Fatalf("build with image error=%v", err)
	}
	if err := runSandbox(&service.App{}, []string{"create", "--build", ".", "--dockerfile", "containers/Dockerfile"}); err == nil || !strings.Contains(err.Error(), "docker not available") {
		t.Fatalf("valid build flags did not reach engine availability check: %v", err)
	}
	if err := runSandbox(&service.App{}, []string{"create", "--remote", "--name", "demo", "--build", "."}); err == nil || !strings.Contains(err.Error(), "local source builds require") {
		t.Fatalf("remote build error=%v", err)
	}
}

func TestSandboxCreateAcceptsMillicoreCPUAndRejectsMalformedCPU(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := runSandbox(&service.App{}, []string{"create", "--cpu", "500m"}); err == nil || strings.Contains(err.Error(), "invalid CPU quantity") {
		t.Fatalf("valid millicore CPU parse error=%v", err)
	}
	if err := runSandbox(&service.App{}, []string{"create", "--cpu", "1cpu"}); err == nil || !strings.Contains(err.Error(), "invalid CPU quantity") {
		t.Fatalf("malformed CPU parse error=%v", err)
	}
	if err := runSandbox(&service.App{}, []string{"create", "--pids-limit", "100oops"}); err == nil || !strings.Contains(err.Error(), "invalid --pids-limit") {
		t.Fatalf("malformed pids limit parse error=%v", err)
	}
}
