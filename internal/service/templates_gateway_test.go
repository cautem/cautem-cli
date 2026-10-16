package service

import (
	"context"
	"net"
	"testing"
	"time"

	datamodelv1 "github.com/NVIDIA/OpenShell/sdk/go/proto/datamodelv1"
	openshellv1 "github.com/NVIDIA/OpenShell/sdk/go/proto/openshellv1"
	"github.com/cautem/cautem-cli/internal/storage/templates"
	"google.golang.org/grpc"
)

type gatewayTemplateCreateTestServer struct {
	openshellv1.UnimplementedOpenShellServer
	request *openshellv1.CreateSandboxTemplateRequest
}

func (s *gatewayTemplateCreateTestServer) CreateSandboxTemplate(_ context.Context, request *openshellv1.CreateSandboxTemplateRequest) (*openshellv1.SandboxTemplateResponse, error) {
	s.request = request
	return &openshellv1.SandboxTemplateResponse{Template: &openshellv1.SandboxWorkloadTemplate{
		Metadata: &datamodelv1.ObjectMeta{Name: request.GetTemplate().GetMetadata().GetName(), Workspace: request.GetWorkspaceScope().GetWorkspace()},
		Spec:     request.GetTemplate().GetSpec(),
	}}, nil
}

func TestGatewayTemplateCreateSendsWorkspaceScopedWorkload(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	created := &gatewayTemplateCreateTestServer{}
	openshellv1.RegisterOpenShellServer(server, created)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	app := &App{GatewayURLOverride: "http://" + listener.Addr().String(), GlobalWorkspace: "team"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	app.SetCommandContext(ctx)
	err = app.GatewayTemplateCreate(templates.Template{
		Name: "shared-agent", Image: "ghcr.io/example/agent:stable", CPU: 0.5, Memory: "2Gi", GPUCount: 2,
		Env: map[string]string{"MODE": "fast"}, Labels: map[string]string{"team": "runtime"},
		DriverConfigJSON: `{"docker":{"network":"isolated"}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.request == nil || created.request.GetWorkspaceScope().GetWorkspace() != "team" {
		t.Fatalf("template request scope = %+v", created.request)
	}
	template := created.request.GetTemplate()
	workload := template.GetSpec().GetWorkload()
	if template.GetMetadata().GetName() != "shared-agent" || template.GetMetadata().GetLabels()["team"] != "runtime" || workload.GetImage() != "ghcr.io/example/agent:stable" || workload.GetResources().GetCpu() != "0.5" || workload.GetResources().GetMemory() != "2Gi" || workload.GetResources().GetGpu().GetCount() != 2 || workload.GetEnvironment()["MODE"] != "fast" || template.GetSpec().GetDriverConfig().AsMap()["docker"] == nil {
		t.Fatalf("template workload mapping = %+v", template)
	}
}
