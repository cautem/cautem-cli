package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrDenyAllUsesCurrentPolicySchema(t *testing.T) {
	_, path, err := (&App{}).loadOrDenyAll("")
	if err != nil {
		t.Fatalf("load default deny-all policy: %v", err)
	}
	defer func() {
		_ = os.Remove(path)
		_ = os.Remove(filepath.Dir(path))
	}()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "network_policies: {}") || strings.Contains(string(body), "\nnetwork:") {
		t.Fatalf("default policy uses stale schema: %s", body)
	}
}
