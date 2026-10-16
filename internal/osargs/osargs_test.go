package osargs_test

import (
	"strings"
	"testing"

	"github.com/cautem/cautem-cli/internal/osargs"
)

func TestParseGatewayAddCompat(t *testing.T) {
	g, err := osargs.ParseGatewayAdd([]string{"http://127.0.0.1:7443", "--local", "--name", "local"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Endpoint != "http://127.0.0.1:7443" || g.Name != "local" || !g.Local {
		t.Fatalf("%+v", g)
	}
}

func TestParseGatewayAddDeriveName(t *testing.T) {
	g, err := osargs.ParseGatewayAdd([]string{"http://127.0.0.1:7443"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Name != "local" || !g.Local {
		t.Fatalf("%+v", g)
	}
}

func TestParseGatewayAddNameURLRejected(t *testing.T) {
	if _, err := osargs.ParseGatewayAdd([]string{"local", "--url", "http://127.0.0.1:7443"}); err == nil {
		t.Fatal("expected error for rejected gateway add NAME --url")
	}
}

func TestParsePolicySet(t *testing.T) {
	p, err := osargs.ParsePolicySet([]string{"demo", "--policy", "/tmp/p.yaml", "--wait"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "demo" || p.Path != "/tmp/p.yaml" || !p.Wait {
		t.Fatalf("%+v", p)
	}
	g, err := osargs.ParsePolicySet([]string{"--global", "--policy", "/tmp/g.yaml", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	if !g.Global || g.Path != "/tmp/g.yaml" || !g.Yes {
		t.Fatalf("%+v", g)
	}
}

func TestParseForwardStart(t *testing.T) {
	f, err := osargs.ParseForwardStart([]string{"8080", "my-app", "-d"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Port != "8080" || f.Name != "my-app" || !f.Background {
		t.Fatalf("%+v", f)
	}
}

func TestParseServiceExpose(t *testing.T) {
	s, err := osargs.ParseServiceExpose([]string{"my-app", "8080", "web"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Sandbox != "my-app" || s.Port != "8080" || s.Service != "web" {
		t.Fatalf("%+v", s)
	}
}

func TestParseSettingsSet(t *testing.T) {
	s, err := osargs.ParseSettingsSet([]string{"demo", "--key", "ocsf_json_enabled", "--value", "true"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "demo" || s.Key != "ocsf_json_enabled" || s.Value != "true" {
		t.Fatalf("%+v", s)
	}
}

func TestParseSandboxTransfer(t *testing.T) {
	u, err := osargs.ParseSandboxTransfer([]string{"demo", "./src", "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "demo" || u.Path != "./src" || u.Dest != "/workspace" {
		t.Fatalf("%+v", u)
	}
	u, err = osargs.ParseSandboxTransfer([]string{"demo", "./src", "--no-git-ignore"})
	if err != nil || !u.NoGitIgnore || u.Name != "demo" || u.Path != "./src" {
		t.Fatalf("upload with no-git-ignore=%+v err=%v", u, err)
	}
	if _, err := osargs.ParseSandboxTransfer([]string{"demo", "./src", "--unexpected"}); err == nil {
		t.Fatal("unknown sandbox transfer flag accepted")
	}
}

func TestParseLogs(t *testing.T) {
	l, err := osargs.ParseLogs([]string{"demo", "--tail", "-n", "50", "--source", "sandbox"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "demo" || !l.Tail || l.N != 50 || !l.NSet || l.Source != "sandbox" {
		t.Fatalf("%+v", l)
	}
	filtered, err := osargs.ParseLogs([]string{"--since", "15m", "--source", "proxy"})
	if err != nil || filtered.Since != "15m" || filtered.Source != "proxy" {
		t.Fatalf("filtered logs = %+v, %v", filtered, err)
	}
	for _, args := range [][]string{{"--source", "unknown"}, {"--since", "tomorrow"}, {"--since", "-1m"}, {"-n", "0"}, {"-n", "4294967296"}} {
		if _, err := osargs.ParseLogs(args); err == nil {
			t.Errorf("ParseLogs(%q) unexpectedly succeeded", args)
		}
	}
}

func TestParseSandboxAttach(t *testing.T) {
	parsed, err := osargs.ParseSandboxAttach([]string{"demo", "--tty"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "demo" || !parsed.TTY {
		t.Fatalf("ParseSandboxAttach()=%#v", parsed)
	}
	for _, args := range [][]string{{}, {"demo", "other"}, {"demo", "--unknown"}} {
		if _, err := osargs.ParseSandboxAttach(args); err == nil {
			t.Errorf("ParseSandboxAttach(%q) unexpectedly succeeded", args)
		}
	}
}

func TestParseSandboxExec(t *testing.T) {
	e, err := osargs.ParseSandboxExec([]string{"--name", "demo", "--workdir", "/tmp", "--env", "MODE=test", "--", "ls", "-la"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "demo" || e.WorkDir != "/tmp" || e.Env["MODE"] != "test" || len(e.Argv) != 2 {
		t.Fatalf("%+v", e)
	}
	e2, err := osargs.ParseSandboxExec([]string{"demo", "--", "uname", "-a"})
	if err != nil {
		t.Fatal(err)
	}
	if e2.Name != "demo" || len(e2.Argv) != 2 {
		t.Fatalf("%+v", e2)
	}
}

func TestParseSandboxExecAllowsImplicitLastUsedTarget(t *testing.T) {
	parsed, err := osargs.ParseSandboxExec([]string{"--", "printf", "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "" || strings.Join(parsed.Argv, " ") != "printf hello" {
		t.Fatalf("implicit target parse = %+v", parsed)
	}
}

func TestParseSandboxExecCompatFlags(t *testing.T) {
	e, err := osargs.ParseSandboxExec([]string{"-n", "demo", "--workdir", "/app", "--timeout", "12", "--no-tty", "--no-login-shell", "--env", "MODE=test", "--", "sh", "-c", "exit 7"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "demo" || e.WorkDir != "/app" || e.TimeoutSec != 12 || e.TTY || !e.TTYSet || !e.NoLoginShell || e.Env["MODE"] != "test" {
		t.Fatalf("parsed exec options=%+v", e)
	}
	if got := strings.Join(e.Argv, " "); got != "sh -c exit 7" {
		t.Fatalf("argv=%q", got)
	}

	forcedTTY, err := osargs.ParseSandboxExec([]string{"demo", "--tty", "--", "cat"})
	if err != nil {
		t.Fatal(err)
	}
	if !forcedTTY.TTY || !forcedTTY.TTYSet {
		t.Fatalf("--tty was not preserved: %+v", forcedTTY)
	}
	lastFlagWins, err := osargs.ParseSandboxExec([]string{"demo", "--tty", "--no-tty", "--", "true"})
	if err != nil {
		t.Fatal(err)
	}
	if lastFlagWins.TTY || !lastFlagWins.TTYSet {
		t.Fatalf("TTY override order was not preserved: %+v", lastFlagWins)
	}
}

func TestParseSandboxExecRejectsInvalidCompatFlags(t *testing.T) {
	for _, args := range [][]string{
		{"demo", "--timeout", "-1", "--", "true"},
		{"demo", "--timeout", "4294967296", "--", "true"},
		{"demo", "--timeout", "one", "--", "true"},
	} {
		if _, err := osargs.ParseSandboxExec(args); err == nil {
			t.Errorf("ParseSandboxExec(%q) unexpectedly succeeded", args)
		}
	}
}
