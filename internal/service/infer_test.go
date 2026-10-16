package service_test

import (
	"testing"

	"github.com/cautem/quarshell-cli/internal/autoprovider"
	"github.com/cautem/quarshell-cli/internal/service"
)

func TestInferProvidersFromArgv(t *testing.T) {
	cases := map[string]string{
		"agent":  "cursor",
		"claude": "claude-code",
		"codex":  "codex",
		"gh":     "github",
	}
	for cmd, want := range cases {
		got := service.InferProvidersFromArgv([]string{cmd})
		if len(got) != 1 || got[0] != want {
			t.Fatalf("%s → %v want %s", cmd, got, want)
		}
	}
	if service.InferProvidersFromArgv(nil) != nil {
		t.Fatal("empty argv")
	}
}

func TestAutoProvidersDefaultOnForKnownAgent(t *testing.T) {
	merged := autoprovider.Merge(nil, service.InferProvidersFromArgv([]string{"agent"}), autoprovider.ModeOn)
	if len(merged) != 1 || merged[0] != "cursor" {
		t.Fatalf("%v", merged)
	}
}
