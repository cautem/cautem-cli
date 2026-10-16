package service

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	core "github.com/cautem/cautem-core"
	"github.com/cautem/cautem-runtime/sandbox"
)

func TestCanonicalMainCommandRequiresNonTTYCreateArgv(t *testing.T) {
	argv := []string{"python", "-c", "print('main')"}
	if got := canonicalMainCommand(nil); len(got) != 0 {
		t.Fatalf("empty argv produced command %q", got)
	}
	got := canonicalMainCommand(argv)
	if len(got) != len(argv) {
		t.Fatalf("canonical command=%q, want %q", got, argv)
	}
	got[0] = "mutated"
	if argv[0] != "python" {
		t.Fatal("canonical command aliases caller argv")
	}
}

func TestCanonicalMainTTYOnlyEnablesPTYForCommand(t *testing.T) {
	if canonicalMainTTY(nil, true) {
		t.Fatal("TTY was enabled for the default idle sandbox")
	}
	if !canonicalMainTTY([]string{"sh"}, true) {
		t.Fatal("TTY was not enabled for an explicitly interactive command")
	}
	if canonicalMainTTY([]string{"sh"}, false) {
		t.Fatal("TTY was enabled without an interactive request")
	}
}

func TestAttachCanonicalMainProcessReturnsExitStatusAndFollowsLogs(t *testing.T) {
	compute := &preflightComputeDriver{mainWaitCode: 17}
	var output bytes.Buffer
	err := attachCanonicalMainProcess(context.Background(), compute, core.ID("main-id"), &output)
	if code, ok := ExitCode(err); !ok || code != 17 {
		t.Fatalf("attachCanonicalMainProcess() error=%v, exit=(%d,%v); want 17", err, code, ok)
	}
	if compute.mainLogsID != core.ID("main-id") {
		t.Fatalf("main-process logs ID=%q; want main-id", compute.mainLogsID)
	}
	if got := output.String(); got != "canonical-output\n" {
		t.Fatalf("attached output=%q; want canonical process output", got)
	}
}

func TestSandboxAttachReconnectsToRunningCanonicalProcess(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	compute := &preflightComputeDriver{mainWaitCode: 17}
	a := &App{Sandboxes: &sandbox.Manager{Driver: compute}}
	var stdout bytes.Buffer
	original := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	callErr := a.SandboxAttach("demo", false)
	_ = write.Close()
	os.Stdout = original
	if callErr == nil {
		t.Fatal("SandboxAttach() error=nil, want canonical exit 17")
	}
	if code, ok := ExitCode(callErr); !ok || code != 17 {
		t.Fatalf("SandboxAttach() error=%v, exit=(%d,%v); want 17", callErr, code, ok)
	}
	if compute.inspectName != "demo" || compute.mainLogsID != core.ID("test-sandbox") {
		t.Fatalf("attach target inspect=%q logs=%q", compute.inspectName, compute.mainLogsID)
	}
	if _, err := io.Copy(&stdout, read); err != nil {
		t.Fatal(err)
	}
	_ = read.Close()
	if stdout.String() != "canonical-output\n" {
		t.Fatalf("reconnected output=%q", stdout.String())
	}
}

func TestSandboxAttachRejectsStoppedSandbox(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	compute := &preflightComputeDriver{inspectStatus: "exited"}
	a := &App{Sandboxes: &sandbox.Manager{Driver: compute}}
	if err := a.SandboxAttach("demo", false); err == nil || !strings.Contains(err.Error(), "requires a running sandbox") {
		t.Fatalf("SandboxAttach() error=%v, want running-state precondition", err)
	}
	if compute.mainLogsID != "" {
		t.Fatalf("stopped sandbox attached to logs %q", compute.mainLogsID)
	}
}

func TestAttachCanonicalMainProcessTTYStreamsInputOutputAndExit(t *testing.T) {
	compute := &ttyMainProcessDriver{preflightComputeDriver: &preflightComputeDriver{mainWaitCode: 23}}
	var output bytes.Buffer
	err := attachCanonicalMainProcessTTY(context.Background(), compute, core.ID("tty-id"), strings.NewReader("input\n"), &output, -1)
	if code, ok := ExitCode(err); !ok || code != 23 {
		t.Fatalf("attachCanonicalMainProcessTTY() error=%v, exit=(%d,%v); want 23", err, code, ok)
	}
	if compute.ttyID != core.ID("tty-id") || compute.ttyCols != 0 || compute.ttyRows != 0 {
		t.Fatalf("TTY attachment target/size=%q/%dx%d", compute.ttyID, compute.ttyCols, compute.ttyRows)
	}
	if got := output.String(); got != "tty-output\n" {
		t.Fatalf("attached TTY output=%q; want tty-output", got)
	}
	select {
	case <-compute.ttySession.writeDone:
	case <-time.After(time.Second):
		t.Fatal("TTY input was not forwarded")
	}
	if got := compute.ttySession.written.String(); got != "input\n" {
		t.Fatalf("attached TTY input=%q; want input", got)
	}
}

type ttyMainProcessDriver struct {
	*preflightComputeDriver
	ttyID      core.ID
	ttyCols    uint32
	ttyRows    uint32
	ttySession *mainProcessTTYBuffer
}

func (d *ttyMainProcessDriver) OpenMainProcessTTY(_ context.Context, id core.ID, cols, rows uint32) (io.ReadWriteCloser, error) {
	d.ttyID, d.ttyCols, d.ttyRows = id, cols, rows
	d.ttySession = &mainProcessTTYBuffer{Reader: strings.NewReader("tty-output\n"), writeDone: make(chan struct{}, 1)}
	return d.ttySession, nil
}

func (*ttyMainProcessDriver) ResizeMainProcessTTY(context.Context, core.ID, uint32, uint32) error {
	return nil
}

type mainProcessTTYBuffer struct {
	*strings.Reader
	written   bytes.Buffer
	writeDone chan struct{}
}

func (s *mainProcessTTYBuffer) Write(p []byte) (int, error) {
	n, err := s.written.Write(p)
	select {
	case s.writeDone <- struct{}{}:
	default:
	}
	return n, err
}
func (*mainProcessTTYBuffer) Close() error { return nil }
