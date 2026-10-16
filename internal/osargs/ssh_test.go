package osargs_test

import (
	"reflect"
	"testing"

	"github.com/cautem/cautem-cli/internal/osargs"
)

func TestParseSSHProxyModes(t *testing.T) {
	cases := []struct {
		args []string
		want osargs.SSHProxy
	}{
		{
			[]string{"--gateway", "https://gw:7443", "--sandbox-id", "demo", "--token", "tok", "--gateway-name", "prod"},
			osargs.SSHProxy{GatewayURL: "https://gw:7443", SandboxID: "demo", Token: "tok", GatewayName: "prod"},
		},
		{
			[]string{"--gateway-name", "local", "--name", "demo"},
			osargs.SSHProxy{GatewayName: "local", Name: "demo"},
		},
		{
			[]string{"-g", "local", "--name", "demo", "--server", "http://127.0.0.1:7443"},
			osargs.SSHProxy{GatewayName: "local", Name: "demo", GatewayURL: "http://127.0.0.1:7443"},
		},
		{
			[]string{"--server", "http://127.0.0.1:7443", "--name", "demo"},
			osargs.SSHProxy{GatewayURL: "http://127.0.0.1:7443", Name: "demo"},
		},
		{
			[]string{"demo"},
			osargs.SSHProxy{Name: "demo"},
		},
		{
			[]string{"--name=demo", "--gateway-name=local"},
			osargs.SSHProxy{Name: "demo", GatewayName: "local"},
		},
	}
	for _, c := range cases {
		got, err := osargs.ParseSSHProxy(c.args)
		if err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%v:\n got %+v\nwant %+v", c.args, got, c.want)
		}
	}
}

func TestParseSSHProxyErrors(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"--token", "tok", "--gateway", "http://x"},
		{"--token", "tok", "--sandbox-id", "demo"},
		{"--bogus", "x", "--name", "demo"},
		{"--name"},
		{"a", "b"},
	} {
		if _, err := osargs.ParseSSHProxy(args); err == nil {
			t.Errorf("%v: expected error", args)
		}
	}
}

func TestParseSandboxConnect(t *testing.T) {
	cases := []struct {
		args []string
		want osargs.SandboxConnect
	}{
		{[]string{"demo"}, osargs.SandboxConnect{Name: "demo"}},
		{[]string{"demo", "--editor", "cursor"}, osargs.SandboxConnect{Name: "demo", Editor: "cursor"}},
		{[]string{"--editor=VSCode", "demo"}, osargs.SandboxConnect{Name: "demo", Editor: "vscode"}},
		{[]string{"demo", "--", "ls", "-la", "--color"}, osargs.SandboxConnect{Name: "demo", Argv: []string{"ls", "-la", "--color"}}},
		{nil, osargs.SandboxConnect{}},
	}
	for _, c := range cases {
		got, err := osargs.ParseSandboxConnect(c.args)
		if err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%v:\n got %+v\nwant %+v", c.args, got, c.want)
		}
	}
	for _, args := range [][]string{
		{"demo", "--ssh"},
		{"demo", "--editor", "emacs"},
		{"demo", "--editor", "cursor", "--", "ls"},
		{"demo", "extra"},
		{"demo", "--editor"},
	} {
		if _, err := osargs.ParseSandboxConnect(args); err == nil {
			t.Errorf("%v: expected error", args)
		}
	}
}

func TestParseSandboxSSHConfig(t *testing.T) {
	got, err := osargs.ParseSandboxSSHConfig([]string{"demo", "--install"})
	if err != nil || got.Name != "demo" || !got.Install {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := osargs.ParseSandboxSSHConfig(nil); err == nil {
		t.Fatal("missing name must fail")
	}
}
