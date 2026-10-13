package service

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	controlv1 "github.com/cautem/cautem-proto/gen/go/cautem/control/v1"
	"google.golang.org/grpc"
)

type policyTestServer struct {
	controlv1.UnimplementedPolicyServiceServer
	approved map[string]bool
}

func (s *policyTestServer) ListPolicyProposals(_ context.Context, _ *controlv1.ListPolicyProposalsRequest) (*controlv1.ListPolicyProposalsResponse, error) {
	return &controlv1.ListPolicyProposalsResponse{Proposals: []*controlv1.PolicyProposalSummary{
		{Id: "ordinary", Status: "pending"}, {Id: "flagged", Status: "pending", SecurityFlagged: true},
	}}, nil
}
func (s *policyTestServer) ApprovePolicyProposal(_ context.Context, req *controlv1.ApprovePolicyProposalRequest) (*controlv1.ApprovePolicyProposalResponse, error) {
	s.approved[req.Id] = true
	return &controlv1.ApprovePolicyProposalResponse{Proposal: &controlv1.PolicyProposalSummary{Id: req.Id, SandboxName: "demo", Status: "approved"}}, nil
}
func (s *policyTestServer) GetPolicyProposal(_ context.Context, req *controlv1.GetPolicyProposalRequest) (*controlv1.GetPolicyProposalResponse, error) {
	return &controlv1.GetPolicyProposalResponse{Proposal: &controlv1.PolicyProposalSummary{Id: req.Id, SandboxName: "demo", Status: "pending"}}, nil
}
func (s *policyTestServer) GetSandboxPolicy(context.Context, *controlv1.GetSandboxPolicyRequest) (*controlv1.GetSandboxPolicyResponse, error) {
	return &controlv1.GetSandboxPolicyResponse{PolicyYaml: "version: 1\nnetwork_policies: {}\n", PolicyRevision: 1}, nil
}

func TestRuleApproveAllGatewayApprovesPendingWithoutClearing(t *testing.T) {
	approved := map[string]bool{}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	controlv1.RegisterPolicyServiceServer(server, &policyTestServer{approved: approved})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	a := &App{GatewayURLOverride: "http://" + listener.Addr().String()}
	if err := a.RuleApproveAll("demo", false); err != nil {
		t.Fatal(err)
	}
	if !approved["ordinary"] || approved["flagged"] {
		t.Fatalf("approved = %v", approved)
	}
	if err := a.RuleApproveAll("demo", true); err != nil {
		t.Fatal(err)
	}
	if !approved["flagged"] {
		t.Fatal("explicit opt-in did not approve flagged proposal")
	}
}

func TestRuleApproveAllLocalPreservesHistoryAndFlaggedRules(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := localParityDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rules.json")
	initial := map[string]any{
		"ordinary": map[string]any{"sandbox": "demo", "status": "pending"},
		"flagged":  map[string]any{"sandbox": "demo", "status": "pending", "security_flagged": true},
		"other":    map[string]any{"sandbox": "other", "status": "pending"},
		"history":  map[string]any{"sandbox": "demo", "status": "rejected"},
	}
	if err := writeJSONMap(path, initial); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	if err := a.ruleApproveAllLocal("demo", false); err != nil {
		t.Fatal(err)
	}
	got, err := readJSONMap(path)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"ordinary": "approved", "flagged": "pending", "other": "pending", "history": "rejected",
	} {
		row := got[id].(map[string]any)
		if row["status"] != want {
			t.Errorf("%s status = %v, want %s", id, row["status"], want)
		}
	}
	if len(got) != len(initial) {
		t.Fatalf("proposal history lost: got %d rows, want %d", len(got), len(initial))
	}
	if err := a.ruleApproveAllLocal("demo", true); err != nil {
		t.Fatal(err)
	}
	got, err = readJSONMap(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["flagged"].(map[string]any)["status"] != "approved" {
		t.Fatal("explicit opt-in did not approve flagged proposal")
	}
}
