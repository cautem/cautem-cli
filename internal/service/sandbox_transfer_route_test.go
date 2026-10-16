package service

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlv1 "github.com/cautem/quarshell-proto/gen/go/cautem/control/v1"
	"github.com/cautem/quarshell-runtime/sandbox"
	"google.golang.org/grpc"
)

type transferRouteGateway struct {
	controlv1.UnimplementedSandboxServiceServer
	name      string
	workspace string
	compute   string
}

func (g *transferRouteGateway) GetSandbox(_ context.Context, req *controlv1.GetSandboxRequest) (*controlv1.GetSandboxResponse, error) {
	g.name = req.GetName()
	g.workspace = req.GetWorkspace()
	return &controlv1.GetSandboxResponse{Sandbox: &controlv1.SandboxSummary{
		Name: req.GetName(), Workspace: req.GetWorkspace(), ComputeDriver: g.compute,
	}}, nil
}

func TestCopyRoutesGatewaySandboxToGatewayBeforeLocalDriver(t *testing.T) {
	server, address, closeServer := startTransferRouteGateway(t)
	defer closeServer()
	driver := &preflightComputeDriver{}
	app := &App{
		GatewayURLOverride: "http://" + address,
		GlobalWorkspace:    "team-a",
		Sandboxes:          &sandbox.Manager{Driver: driver},
	}
	localDest := filepath.Join(t.TempDir(), "downloaded")
	err := app.Copy("shared:/workspace/report.txt", localDest)
	if err == nil || !strings.Contains(err.Error(), "Gateway download") {
		t.Fatalf("copy error=%v; want Gateway download exec route", err)
	}
	if server.name != "shared" || server.workspace != "team-a" {
		t.Fatalf("Gateway lookup targeted %q in workspace %q", server.name, server.workspace)
	}
	if driver.inspectName != "" || driver.copyFromCalls != 0 || len(driver.copyEntries) != 0 {
		t.Fatalf("local driver was used for shared copy: %+v", driver)
	}
}

func TestSandboxUploadRoutesGatewaySandboxBeforeLocalDriver(t *testing.T) {
	server, address, closeServer := startTransferRouteGateway(t)
	defer closeServer()
	driver := &preflightComputeDriver{}
	app := &App{
		GatewayURLOverride: "http://" + address,
		GlobalWorkspace:    "team-a",
		Sandboxes:          &sandbox.Manager{Driver: driver},
	}
	source := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(source, []byte("upload"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := app.SandboxUpload(source, "shared:/workspace", true)
	if err == nil {
		t.Fatal("upload unexpectedly succeeded without a Gateway exec service")
	}
	if server.name != "shared" || server.workspace != "team-a" {
		t.Fatalf("Gateway lookup targeted %q in workspace %q", server.name, server.workspace)
	}
	if driver.inspectName != "" || driver.copyFromCalls != 0 || len(driver.copyEntries) != 0 {
		t.Fatalf("local driver was used for shared upload: %+v", driver)
	}
}

func TestCopyKeepsCLIRuntimeOnLocalDriver(t *testing.T) {
	server, address, closeServer := startTransferRouteGateway(t)
	defer closeServer()
	server.compute = "cli-managed"
	driver := &preflightComputeDriver{}
	app := &App{
		GatewayURLOverride: "http://" + address,
		GlobalWorkspace:    "team-a",
		Sandboxes:          &sandbox.Manager{Driver: driver},
	}
	if err := app.Copy("managed:/workspace/report.txt", filepath.Join(t.TempDir(), "report.txt")); err != nil {
		t.Fatalf("copy from CLI-managed sandbox: %v", err)
	}
	if driver.inspectName != "managed" || driver.copyFromCalls != 1 {
		t.Fatalf("CLI-managed copy route: inspect=%q copyFrom=%d", driver.inspectName, driver.copyFromCalls)
	}
}

func startTransferRouteGateway(t *testing.T) (*transferRouteGateway, string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &transferRouteGateway{compute: "docker"}
	grpcServer := grpc.NewServer()
	controlv1.RegisterSandboxServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	return server, listener.Addr().String(), func() {
		grpcServer.Stop()
		_ = listener.Close()
	}
}
