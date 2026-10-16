package service

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	controlv1 "github.com/cautem/quarshell-proto/gen/go/cautem/control/v1"
	"google.golang.org/grpc"
)

type remoteCreateTestServer struct {
	controlv1.UnimplementedSandboxServiceServer
	request      *controlv1.CreateSandboxRequest
	attachStarts int
}

func (s *remoteCreateTestServer) AttachMainProcess(stream grpc.BidiStreamingServer[controlv1.AttachMainProcessRequest, controlv1.AttachMainProcessResponse]) error {
	start, err := stream.Recv()
	if err != nil {
		return err
	}
	if start.GetStart() == nil || start.GetStart().GetName() != "agent" {
		return fmt.Errorf("unexpected attach start: %+v", start)
	}
	s.attachStarts++
	if err := stream.Send(&controlv1.AttachMainProcessResponse{Payload: &controlv1.AttachMainProcessResponse_Output{Output: []byte("foreground-output\n")}}); err != nil {
		return err
	}
	return stream.Send(&controlv1.AttachMainProcessResponse{Payload: &controlv1.AttachMainProcessResponse_ExitCode{ExitCode: 0}})
}

func (s *remoteCreateTestServer) CreateSandbox(_ context.Context, request *controlv1.CreateSandboxRequest) (*controlv1.CreateSandboxResponse, error) {
	s.request = request
	return &controlv1.CreateSandboxResponse{Completed: true, OperationId: "operation-remote", Sandbox: &controlv1.SandboxSummary{
		Name: request.GetName(), Workspace: request.GetWorkspace(), RegistryStatus: "running", ComputeDriver: "docker", Image: request.GetImage(),
	}}, nil
}

func (*remoteCreateTestServer) GetSandbox(_ context.Context, request *controlv1.GetSandboxRequest) (*controlv1.GetSandboxResponse, error) {
	return &controlv1.GetSandboxResponse{Sandbox: &controlv1.SandboxSummary{Name: request.GetName(), Workspace: request.GetWorkspace(), ComputeDriver: "docker", RegistryStatus: "running"}}, nil
}

func TestRemoteSandboxCreateValidatesContractBeforeGatewayAccess(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app := &App{}
	for _, test := range []struct {
		name string
		opt  SandboxCreateOpts
		want string
	}{
		{name: "requires name", opt: SandboxCreateOpts{Remote: true, Image: "busybox"}, want: "--name is required"},
		{name: "rejects host bind mount", opt: SandboxCreateOpts{Remote: true, Name: "agent", Image: "busybox", WorkspaceFlagSet: true}, want: "--workspace is a local bind mount"},
		{name: "requires image or template", opt: SandboxCreateOpts{Remote: true, Name: "agent"}, want: "provide --image"},
		{name: "rejects negative cpu before gateway", opt: SandboxCreateOpts{Remote: true, Name: "agent", Image: "busybox", CPU: -1}, want: "finite non-negative"},
		{name: "rejects excessive gpu count before gateway", opt: SandboxCreateOpts{Remote: true, Name: "agent", Image: "busybox", GPUCount: 1025}, want: "between 0 and 1024"},
		{name: "rejects invalid remote pids limit before gateway", opt: SandboxCreateOpts{Remote: true, Name: "agent", Image: "busybox", PidsLimit: -2}, want: "--pids-limit must be -1 or a positive integer"},
		{name: "rejects mismatched remote CDI count before gateway", opt: SandboxCreateOpts{Remote: true, Name: "agent", Image: "busybox", GPU: true, GPUCount: 2, CDIDevices: []string{"nvidia.com/gpu=0"}}, want: "does not match 1 explicit CDI devices"},
		{name: "rejects template overrides", opt: SandboxCreateOpts{Remote: true, Name: "agent", Template: "shared", CPU: 1}, want: "--template cannot be combined with workload options"},
		{name: "rejects local-only alias image", opt: SandboxCreateOpts{Remote: true, Name: "agent", From: "base"}, want: "local-only image"},
		{name: "requires selected gateway", opt: SandboxCreateOpts{Remote: true, Name: "agent", Image: "busybox"}, want: "no current gateway"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := app.SandboxCreate(test.opt)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("SandboxCreate error=%v, want substring %q", err, test.want)
			}
		})
	}
}

func TestRemoteSandboxCreateCarriesWorkloadOptions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	created := &remoteCreateTestServer{}
	controlv1.RegisterSandboxServiceServer(server, created)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	app := &App{GatewayURLOverride: "http://" + listener.Addr().String(), GlobalWorkspace: "team"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	app.SetCommandContext(ctx)
	err = app.sandboxCreateRemote(SandboxCreateOpts{
		Remote: true, Name: "agent", Image: "alpine:3.22", CPU: .5, Memory: "2Gi", GPU: true, GPUCount: 2,
		PidsLimit: 512, CDIDevices: []string{"nvidia.com/gpu=0", "nvidia.com/gpu=1"},
		Env: map[string]string{"FEATURE_FLAG": "on"}, DriverConfigJSON: `{"docker":{"shm_size":67108864}}`,
		ForceTTY: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.request == nil || created.request.GetCpuMillis() != 500 || created.request.GetMemoryBytes() != 2*1024*1024*1024 || created.request.GetGpuCount() != 2 || created.request.GetPidsLimit() != 512 || strings.Join(created.request.GetCdiDevices(), ",") != "nvidia.com/gpu=0,nvidia.com/gpu=1" || created.request.GetEnvironment()["FEATURE_FLAG"] != "on" || created.request.GetDriverConfigJson() == "" || !created.request.GetTty() {
		t.Fatalf("remote create dropped workload options: %+v", created.request)
	}
}

func TestRemoteSandboxCreateForegroundAttachesToCanonicalProcess(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	created := &remoteCreateTestServer{}
	controlv1.RegisterSandboxServiceServer(server, created)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	app := &App{GatewayURLOverride: "http://" + listener.Addr().String(), GlobalWorkspace: "team"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	app.SetCommandContext(ctx)
	if err := app.sandboxCreateRemote(SandboxCreateOpts{Remote: true, Name: "agent", Image: "alpine:3.22", Argv: []string{"sh", "-c", "printf hello"}}); err != nil {
		t.Fatal(err)
	}
	if created.request == nil || len(created.request.GetCommand()) != 3 || created.attachStarts != 1 {
		t.Fatalf("foreground remote create request=%+v attachStarts=%d", created.request, created.attachStarts)
	}
}

func TestSandboxAttachRoutesGatewayManagedSandboxToControlStream(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	backend := &remoteCreateTestServer{}
	controlv1.RegisterSandboxServiceServer(server, backend)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	app := &App{GatewayURLOverride: "http://" + listener.Addr().String(), GlobalWorkspace: "team"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	app.SetCommandContext(ctx)
	if err := app.SandboxAttach("agent", false); err != nil {
		t.Fatal(err)
	}
	if backend.attachStarts != 1 {
		t.Fatalf("Gateway attach started %d streams, want 1", backend.attachStarts)
	}
}

func TestRemoteSandboxCreateCallsGatewayControlAndKeepsTemplateScope(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	created := &remoteCreateTestServer{}
	controlv1.RegisterSandboxServiceServer(server, created)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	app := &App{GatewayURLOverride: "http://" + listener.Addr().String(), GlobalWorkspace: "team"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	app.SetCommandContext(ctx)
	err = app.sandboxCreateRemote(SandboxCreateOpts{
		Remote: true, Name: "agent", Template: "shared-agent", Detach: true,
		Argv: []string{"sleep", "infinity"}, Labels: map[string]string{"purpose": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.request == nil || created.request.GetWorkspace() != "team" || created.request.GetName() != "agent" || created.request.GetWorkloadTemplateName() != "shared-agent" || created.request.GetImage() != "" {
		t.Fatalf("unexpected remote create request: %+v", created.request)
	}
	if len(created.request.GetCommand()) != 2 || created.request.GetCommand()[0] != "sleep" || created.request.GetLabels()["purpose"] != "test" {
		t.Fatalf("remote create request lost command/labels: %+v", created.request)
	}
}
