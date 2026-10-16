package cli

import (
	"testing"

	"github.com/cautem/cautem-cli/internal/service"
)

func TestCommandRoutersRejectIgnoredPositionalArguments(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "doctor check", run: func() error { return runDoctor(nil, []string{"check", "ignored"}) }},
		{name: "workspace member list extra", run: func() error {
			return runWorkspace(nil, []string{"member", "list", "--workspace", "team", "other"})
		}},
		{name: "workspace member list missing flag value", run: func() error {
			return runWorkspace(nil, []string{"member", "list", "--workspace", "--output", "json"})
		}},
		{name: "workspace create duplicate name", run: func() error {
			return runWorkspace(nil, []string{"create", "--name", "one", "--name", "two"})
		}},
		{name: "workspace member duplicate flag", run: func() error {
			return runWorkspace(nil, []string{"member", "add", "--workspace", "one", "--workspace", "two", "--subject", "alice"})
		}},
		{name: "workspace member duplicate positional and flag", run: func() error {
			return runWorkspace(nil, []string{"member", "add", "one", "--workspace", "two"})
		}},
		{name: "workspace member remove role", run: func() error {
			return runWorkspace(nil, []string{"member", "remove", "--workspace", "one", "--subject", "alice", "--role", "admin"})
		}},
		{name: "version", run: func() error { return execute(service.New(), []string{"version", "ignored"}) }},
		{name: "health", run: func() error { return execute(service.New(), []string{"health", "ignored"}) }},
		{name: "status", run: func() error { return execute(service.New(), []string{"status", "ignored"}) }},
		{name: "whoami", run: func() error { return execute(service.New(), []string{"whoami", "ignored"}) }},
		{name: "terminal", run: func() error { return runTerm(nil, []string{"ignored"}) }},
		{name: "completions", run: func() error { return runCompletions([]string{"bash", "ignored"}) }},
		{name: "sandbox get", run: func() error { return runSandbox(nil, []string{"get", "demo", "ignored"}) }},
		{name: "sandbox provider list", run: func() error { return runSandbox(nil, []string{"provider", "list", "demo", "ignored"}) }},
		{name: "sandbox provider attach", run: func() error { return runSandbox(nil, []string{"provider", "attach", "demo", "github", "ignored"}) }},
		{name: "sandbox provider detach", run: func() error { return runSandbox(nil, []string{"provider", "detach", "demo", "github", "ignored"}) }},
		{name: "gateway ensure", run: func() error { return runGateway(nil, []string{"ensure", "ignored"}) }},
		{name: "gateway remove", run: func() error { return runGateway(nil, []string{"remove", "gateway", "ignored"}) }},
		{name: "gateway select", run: func() error { return runGateway(nil, []string{"select", "gateway", "ignored"}) }},
		{name: "gateway status", run: func() error { return runGateway(nil, []string{"status", "ignored"}) }},
		{name: "gateway info", run: func() error { return runGateway(nil, []string{"info", "ignored"}) }},
		{name: "gateway list", run: func() error { return runGateway(nil, []string{"list", "ignored"}) }},
		{name: "gateway login", run: func() error { return runGateway(nil, []string{"login", "token", "ignored"}) }},
		{name: "gateway logout", run: func() error { return runGateway(nil, []string{"logout", "ignored"}) }},
		{name: "workspace list", run: func() error { return runWorkspace(nil, []string{"list", "ignored"}) }},
		{name: "forward list", run: func() error { return runForward(nil, []string{"list", "ignored"}) }},
		{name: "service list", run: func() error { return runService(nil, []string{"list", "ignored"}) }},
		{name: "rule history", run: func() error { return runRule(nil, []string{"history", "demo", "ignored"}) }},
		{name: "rule clear", run: func() error { return runRule(nil, []string{"clear", "ignored"}) }},
		{name: "rule list", run: func() error { return runRule(nil, []string{"list", "demo", "other"}) }},
		{name: "inference get", run: func() error { return runInference(nil, []string{"get", "ignored"}) }},
		{name: "inference list", run: func() error { return runInference(nil, []string{"list", "ignored"}) }},
		{name: "inference show", run: func() error { return runInference(nil, []string{"show", "one", "two"}) }},
		{name: "inference local", run: func() error { return runInference(nil, []string{"local", "ignored"}) }},
		{name: "inference delete", run: func() error { return runInference(nil, []string{"delete", "ignored"}) }},
		{name: "policy check", run: func() error { return runPolicy(nil, []string{"check", "one.yaml", "ignored"}) }},
		{name: "policy list", run: func() error { return runPolicy(nil, []string{"list", "demo", "other"}) }},
		{name: "policy global list with name", run: func() error { return runPolicy(nil, []string{"list", "--global", "demo"}) }},
		{name: "policy delete unknown", run: func() error { return runPolicy(nil, []string{"delete", "--global", "ignored"}) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("command unexpectedly accepted extra arguments")
			}
		})
	}
}

func TestParseRuleActionRejectsExtraArgumentsAndDuplicateFlags(t *testing.T) {
	for _, args := range [][]string{
		{"chunk-1", "chunk-2"},
		{"--chunk-id", "chunk-1", "--chunk-id", "chunk-2"},
		{"chunk-1", "--reason", "why", "--reason", "again"},
	} {
		t.Run(args[0], func(t *testing.T) {
			if _, _, err := parseRuleAction(args); err == nil {
				t.Fatalf("parseRuleAction(%q) unexpectedly succeeded", args)
			}
		})
	}
}
