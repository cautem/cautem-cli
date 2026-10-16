package global_test

import (
	"testing"

	"github.com/cautem/quarshell-cli/internal/global"
)

func TestParseGatewayAndOutput(t *testing.T) {
	ctx, rest := global.Parse([]string{"-g", "local", "-o", "json", "status"})
	if ctx.GatewayName != "local" || ctx.Output != "json" {
		t.Fatalf("%+v", ctx)
	}
	if len(rest) != 1 || rest[0] != "status" {
		t.Fatalf("%v", rest)
	}
}

func TestParseWorkspacePreservesSandboxFlag(t *testing.T) {
	ctx, rest := global.Parse([]string{"sandbox", "create", "--workspace", "/tmp/proj"})
	if ctx.Workspace != "default" && ctx.Workspace == "" {
		t.Fatalf("workspace should stay default when flag is after command: %+v", ctx)
	}
	if len(rest) < 3 || rest[0] != "sandbox" {
		t.Fatalf("%v", rest)
	}
}

func TestParseLeadingWorkspace(t *testing.T) {
	ctx, rest := global.Parse([]string{"--workspace", "team-ml", "sandbox", "list"})
	if ctx.Workspace != "team-ml" {
		t.Fatalf("%+v", ctx)
	}
	if rest[0] != "sandbox" {
		t.Fatalf("%v", rest)
	}
}
