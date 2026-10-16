package service

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeHelper(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveLinuxHelperPinnedDir(t *testing.T) {
	dir := t.TempDir()
	want := writeHelper(t, dir, helperSSHD)
	t.Setenv(EnvHelpersDir, dir)

	built := false
	build := func(context.Context, string) (string, error) { built = true; return "", nil }

	got, err := resolveLinuxHelper(context.Background(), helperSSHD, "github.com/cautem/quarshell-runtime", build)
	if err != nil || got != want || built {
		t.Fatalf("got %q, %v (built=%v); want %q", got, err, built, want)
	}

	_, err = resolveLinuxHelper(context.Background(), helperInit, "github.com/cautem/quarshell-runtime", build)
	if err == nil || built || !strings.Contains(err.Error(), EnvHelpersDir) {
		t.Fatalf("pinned dir must not fall back to source builds: err=%v built=%v", err, built)
	}
}

func TestResolveLinuxHelperIgnoresEmptyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, helperInit), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvHelpersDir, dir)
	if _, ok := bundledHelper(helperInit); ok {
		t.Fatal("empty helper file accepted")
	}
}

func TestHelperDirsLayout(t *testing.T) {
	t.Setenv(EnvHelpersDir, "")
	dirs := helperDirs()
	if len(dirs) != 2 {
		t.Fatalf("dirs = %q", dirs)
	}
	sub := filepath.Join("libexec", "cautem", "linux-"+runtime.GOARCH)
	for _, d := range dirs {
		if !strings.HasSuffix(d, sub) {
			t.Fatalf("%q does not end with %q", d, sub)
		}
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	if want := filepath.Join(filepath.Dir(filepath.Dir(exe)), sub); filepath.Clean(dirs[0]) != want {
		t.Fatalf("prefix layout: %q want %q", dirs[0], want)
	}
}
