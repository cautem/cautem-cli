package gwconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("current: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := File{Current: "dev", Gateways: map[string]Gateway{"dev": {URL: "http://127.0.0.1:7443", Token: "secret", RefreshToken: "r"}}}
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}

	out, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.Current != "dev" || out.Gateways["dev"].Token != "secret" {
		t.Fatalf("round-trip: %+v", out)
	}
}

func TestResolveImageExperimentalGUIGPU(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CAUTEM_USE_GHCR", "")
	local, err := ResolveImage("gui-gpu", "")
	if err != nil {
		t.Fatal(err)
	}
	if local != "cautem-sandbox:gui-gpu" {
		t.Fatalf("local GUI+GPU image = %q", local)
	}

	t.Setenv("CAUTEM_USE_GHCR", "1")
	remote, err := ResolveImage("cautem/gui-gpu", "")
	if err != nil {
		t.Fatal(err)
	}
	if remote != "ghcr.io/cautem/cautem/sandboxes/gui-gpu:latest" {
		t.Fatalf("published GUI+GPU image = %q", remote)
	}
}
