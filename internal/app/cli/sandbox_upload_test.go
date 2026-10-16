package cli

import (
	"strings"
	"testing"

	"github.com/cautem/cautem-cli/internal/service"
)

func TestSandboxNoGitIgnoreFlagsAreScopedToUploads(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENSHELL_GATEWAY", "")
	t.Setenv("CAUTEM_GATEWAY_URL", "")
	t.Setenv("CAUTEM_GATEWAY_TOKEN", "")
	if err := runSandbox(&service.App{}, []string{"create", "--no-git-ignore"}); err == nil || !strings.Contains(err.Error(), "requires --upload") {
		t.Fatalf("create without --upload error=%v; want flag dependency", err)
	}
	if err := runSandbox(&service.App{}, []string{"download", "demo", "/workspace", "--no-git-ignore"}); err == nil || !strings.Contains(err.Error(), "only valid") {
		t.Fatalf("download with --no-git-ignore error=%v; want upload-only validation", err)
	}
	if err := runSandbox(&service.App{}, []string{"upload", "demo", "./src", "--no-git-ignore"}); err == nil || !strings.Contains(err.Error(), "docker not available") {
		t.Fatalf("upload flag was not parsed; error=%v", err)
	}
}
