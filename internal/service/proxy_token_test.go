package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSandboxTokenFromEnvironment(t *testing.T) {
	t.Run("environment value takes precedence", func(t *testing.T) {
		t.Setenv(EnvSandboxToken, " env-token ")
		t.Setenv(EnvSandboxTokenFile, filepath.Join(t.TempDir(), "missing"))

		got, err := sandboxTokenFromEnvironment()
		if err != nil || got != "env-token" {
			t.Fatalf("sandboxTokenFromEnvironment() = %q, %v; want env-token", got, err)
		}
	})

	t.Run("reads and trims file value", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sandbox-token")
		if err := os.WriteFile(path, []byte("\n file-token \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(EnvSandboxToken, "")
		t.Setenv(EnvSandboxTokenFile, path)

		got, err := sandboxTokenFromEnvironment()
		if err != nil || got != "file-token" {
			t.Fatalf("sandboxTokenFromEnvironment() = %q, %v; want file-token", got, err)
		}
	})

	t.Run("no source is optional", func(t *testing.T) {
		t.Setenv(EnvSandboxToken, "")
		t.Setenv(EnvSandboxTokenFile, "")

		got, err := sandboxTokenFromEnvironment()
		if err != nil || got != "" {
			t.Fatalf("sandboxTokenFromEnvironment() = %q, %v; want empty token", got, err)
		}
	})

	t.Run("rejects relative file path", func(t *testing.T) {
		t.Setenv(EnvSandboxToken, "")
		t.Setenv(EnvSandboxTokenFile, "token")

		if _, err := sandboxTokenFromEnvironment(); err == nil {
			t.Fatal("sandboxTokenFromEnvironment() accepted a relative path")
		}
	})

	t.Run("rejects empty file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sandbox-token")
		if err := os.WriteFile(path, []byte(" \n\t "), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(EnvSandboxToken, "")
		t.Setenv(EnvSandboxTokenFile, path)

		if _, err := sandboxTokenFromEnvironment(); err == nil {
			t.Fatal("sandboxTokenFromEnvironment() accepted an empty token file")
		}
	})
}
