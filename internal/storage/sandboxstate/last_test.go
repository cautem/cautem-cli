package sandboxstate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLastSandboxIsScopedToGatewayAndWorkspace(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SetLast("gateway-a", "team-a", "sandbox-one"); err != nil {
		t.Fatal(err)
	}
	if err := SetLast("gateway-a", "team-b", "sandbox-two"); err != nil {
		t.Fatal(err)
	}
	if err := SetLast("gateway-b", "team-a", "sandbox-three"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ gateway, workspace, want string }{
		{"gateway-a", "team-a", "sandbox-one"},
		{"gateway-a", "team-b", "sandbox-two"},
		{"gateway-b", "team-a", "sandbox-three"},
		{"gateway-b", "team-b", ""},
	} {
		got, err := GetLast(tc.gateway, tc.workspace)
		if err != nil || got != tc.want {
			t.Errorf("GetLast(%q, %q) = %q, %v; want %q", tc.gateway, tc.workspace, got, err, tc.want)
		}
	}
}

func TestLastSandboxFileIsOwnerOnlyAndAtomic(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	if err := SetLast("gw", "default", "demo"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(configHome, "cautem", "last-sandbox.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %o, want 600", info.Mode().Perm())
	}
}
