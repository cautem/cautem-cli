package service

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestLocalExecArgvPreservesArgumentsAndSelectsShellMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local shell integration test requires a POSIX host")
	}
	payload := `quote' ; printf injected >&2; # $HOME *`
	argv := localExecArgv([]string{"printf", "%s", payload}, true)
	if len(argv) != 5 || argv[0] != "/bin/sh" || argv[1] != "-c" || !strings.Contains(argv[2], "/bin/bash -c") || !strings.Contains(argv[4], `"'"'`) {
		t.Fatalf("no-login argv = %#v", argv)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("execute quoted argv: %v: %s", err, output)
	}
	if string(output) != payload {
		t.Fatalf("command output = %q, want literal payload %q", output, payload)
	}

	loginArgv := localExecArgv([]string{"true"}, false)
	if !strings.Contains(loginArgv[2], "/bin/bash -lc") || !strings.Contains(loginArgv[2], "/bin/sh -lc") {
		t.Fatalf("default shell selector does not use login mode: %q", loginArgv[2])
	}
	if output, err := exec.Command(loginArgv[0], loginArgv[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("execute login-shell argv: %v: %s", err, output)
	}
}
