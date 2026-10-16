package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderHostBlockMatchesCompatShape(t *testing.T) {
	pc := ProxyCommand("/usr/local/bin/cautem", "--gateway-name", "local", "--name", "demo")
	got := RenderHostBlock(Alias("demo"), pc)
	want := `Host cautem-demo
    User sandbox
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    GlobalKnownHostsFile /dev/null
    LogLevel ERROR
    ServerAliveInterval 15
    ServerAliveCountMax 3
    ForwardAgent no
    ForwardX11 no
    ProxyCommand /usr/local/bin/cautem ssh-proxy --gateway-name local --name demo
`
	if got != want {
		t.Fatalf("block mismatch:\n%s\nwant:\n%s", got, want)
	}
}

func TestQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/bin/cautem":             "/usr/bin/cautem",
		"/Applications/My App/cautem": `"/Applications/My App/cautem"`,
		`C:\Program Files\cautem.exe`: `"C:\\Program Files\\cautem.exe"`,
		"https://gw.example:7443":     "https://gw.example:7443",
		"a;rm -rf /":                  `"a;rm -rf /"`,
		"":                            `""`,
	} {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestUpsertHostBlock(t *testing.T) {
	b1 := RenderHostBlock("cautem-a", "x ssh-proxy --name a")
	b2 := RenderHostBlock("cautem-b", "x ssh-proxy --name b")
	s := UpsertHostBlock("", "cautem-a", b1)
	s = UpsertHostBlock(s, "cautem-b", b2)
	if strings.Count(s, "Host cautem-") != 2 {
		t.Fatalf("want 2 hosts:\n%s", s)
	}
	b1new := RenderHostBlock("cautem-a", "y ssh-proxy --name a")
	s2 := UpsertHostBlock(s, "cautem-a", b1new)
	if strings.Contains(s2, "x ssh-proxy --name a") || !strings.Contains(s2, "y ssh-proxy --name a") {
		t.Fatalf("block not replaced:\n%s", s2)
	}
	if !strings.Contains(s2, "x ssh-proxy --name b") {
		t.Fatalf("other block damaged:\n%s", s2)
	}
	if UpsertHostBlock(s2, "cautem-a", b1new) != s2 {
		t.Fatal("upsert is not idempotent")
	}
	// A host whose alias only shares a prefix must not be touched.
	s3 := UpsertHostBlock("Host cautem-ab\n    User x\n", "cautem-a", b1)
	if !strings.Contains(s3, "Host cautem-ab\n    User x") || !strings.Contains(s3, "Host cautem-a\n") {
		t.Fatalf("prefix host mishandled:\n%s", s3)
	}
}

func TestRemoveHostBlock(t *testing.T) {
	s := UpsertHostBlock("", "cautem-a", RenderHostBlock("cautem-a", "p"))
	s = UpsertHostBlock(s, "cautem-b", RenderHostBlock("cautem-b", "p"))
	s = RemoveHostBlock(s, "cautem-a")
	if strings.Contains(s, "cautem-a") || !strings.Contains(s, "Host cautem-b") {
		t.Fatalf("remove:\n%s", s)
	}
}

func TestEnsureIncludeBeforeFirstHost(t *testing.T) {
	user := "# personal\nServerAliveInterval 30\n\nHost github.com\n    User git\n"
	got := EnsureInclude(user, "/home/u/.config/cautem/ssh_config")
	inc := strings.Index(got, "Include /home/u/.config/cautem/ssh_config")
	host := strings.Index(got, "Host github.com")
	if inc < 0 || host < 0 || inc > host {
		t.Fatalf("include must precede first Host:\n%s", got)
	}
	if EnsureInclude(got, "/home/u/.config/cautem/ssh_config") != got {
		t.Fatal("EnsureInclude is not idempotent")
	}
	if got := EnsureInclude("", "/p"); got != "Include /p\n" {
		t.Fatalf("empty config = %q", got)
	}
	if got := EnsureInclude("Match host x\n  User y\n", "/p"); !strings.HasPrefix(got, "Include /p\n") {
		t.Fatalf("Match not treated as block start: %q", got)
	}
	quoted := `Include "/My Dir/ssh_config"` + "\n"
	if EnsureInclude(quoted, "/My Dir/ssh_config") != quoted {
		t.Fatal("quoted include not detected")
	}
}

func TestInstallWritesManagedFiles(t *testing.T) {
	dir := t.TempDir()
	p := Paths{Managed: filepath.Join(dir, "cfg", "cautem", "ssh_config"), User: filepath.Join(dir, "ssh", "config")}
	if err := os.MkdirAll(filepath.Dir(p.User), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.User, []byte("Host box\n    User me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	block := RenderHostBlock("cautem-demo", "w ssh-proxy --name demo")
	if err := Install(p, "cautem-demo", block); err != nil {
		t.Fatal(err)
	}
	if err := Install(p, "cautem-demo", block); err != nil {
		t.Fatal(err)
	}
	m, _ := os.ReadFile(p.Managed)
	if strings.Count(string(m), "Host cautem-demo") != 1 {
		t.Fatalf("managed:\n%s", m)
	}
	u, _ := os.ReadFile(p.User)
	if strings.Count(string(u), "Include") != 1 || !strings.HasPrefix(string(u), "Include ") {
		t.Fatalf("user config:\n%s", u)
	}
	if err := Uninstall(p, "cautem-demo"); err != nil {
		t.Fatal(err)
	}
	m, _ = os.ReadFile(p.Managed)
	if strings.Contains(string(m), "cautem-demo") {
		t.Fatalf("uninstall left block:\n%s", m)
	}
}

func TestEnsureIncludeWindowsPathIdempotent(t *testing.T) {
	managed := `C:\Users\RUNNER~1\AppData\Local\cautem\ssh_config`
	once := EnsureInclude("Host box\n    User me\n", managed)
	if !strings.Contains(once, `Include "C:\\Users\\RUNNER~1`) {
		t.Fatalf("backslashes must be escaped inside quotes:\n%s", once)
	}
	if twice := EnsureInclude(once, managed); twice != once {
		t.Fatalf("second EnsureInclude changed content:\n%s", twice)
	}
}
