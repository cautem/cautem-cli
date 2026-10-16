package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/cautem/quarshell-cli/internal/storage/sandboxstate"
	"github.com/cautem/quarshell-sdk/go/cautem"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRemoveSandboxesAttemptsEveryName(t *testing.T) {
	var removed []string
	wantErr := errors.New("container busy")
	err := removeSandboxes([]string{"one", "two", "three"}, func(name string) error {
		removed = append(removed, name)
		if name == "two" {
			return wantErr
		}
		return nil
	})
	if !reflect.DeepEqual(removed, []string{"one", "two", "three"}) {
		t.Fatalf("remove order = %v", removed)
	}
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "failed to remove 1 of 3") || !strings.Contains(err.Error(), "two") {
		t.Fatalf("removeSandboxes() error = %v", err)
	}
}

func TestResolveSandboxTargetUsesGatewayWorkspaceScope(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app := &App{GatewayURLOverride: "https://gw.example", GlobalWorkspace: "team-a"}
	if err := sandboxstate.SetLast("https://gw.example", "team-a", "remembered"); err != nil {
		t.Fatal(err)
	}
	got, err := app.resolveSandboxTarget("")
	if err != nil || got != "remembered" {
		t.Fatalf("resolveSandboxTarget() = %q, %v; want remembered", got, err)
	}
	if got, err := app.resolveSandboxTarget("explicit"); err != nil || got != "explicit" {
		t.Fatalf("resolve explicit target = %q, %v", got, err)
	}
	app.GlobalWorkspace = "team-b"
	if _, err := app.resolveSandboxTarget(""); err == nil || !strings.Contains(err.Error(), "no last-used sandbox") {
		t.Fatalf("missing scoped target error = %v", err)
	}
}

func TestLifecycleRouteSeparatesGatewayAndCLIRuntimes(t *testing.T) {
	if !isCLIRuntimeSandbox(cautem.Sandbox{ComputeDriver: "cli-managed"}) {
		t.Fatal("cli-managed sandbox should use local runtime lifecycle")
	}
	for _, driver := range []string{"docker", "kubernetes", "vm", ""} {
		if isCLIRuntimeSandbox(cautem.Sandbox{ComputeDriver: driver}) {
			t.Errorf("compute driver %q was incorrectly routed to local CLI runtime", driver)
		}
	}
	if !lifecycleLookupAllowsLocalFallback(connect.NewError(connect.CodeNotFound, errors.New("missing"))) {
		t.Fatal("NotFound should permit local unmanaged sandbox fallback")
	}
	if !lifecycleLookupAllowsLocalFallback(status.Error(codes.NotFound, "missing")) {
		t.Fatal("gRPC NotFound should permit local unmanaged sandbox fallback")
	}
	if lifecycleLookupAllowsLocalFallback(status.Error(codes.Unavailable, "gateway down")) {
		t.Fatal("gRPC Unavailable must not permit local fallback")
	}
	for _, code := range []connect.Code{connect.CodePermissionDenied, connect.CodeUnavailable, connect.CodeUnauthenticated} {
		if lifecycleLookupAllowsLocalFallback(connect.NewError(code, errors.New("gateway error"))) {
			t.Errorf("gateway error %s must fail closed instead of falling back locally", code)
		}
	}
}

func TestRemoveSandboxesEmptyIsNoop(t *testing.T) {
	called := false
	if err := removeSandboxes(nil, func(string) error { called = true; return nil }); err != nil {
		t.Fatalf("removeSandboxes(empty) error = %v", err)
	}
	if called {
		t.Fatal("remove callback called for empty input")
	}
}
