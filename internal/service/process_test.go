package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cautem/quarshell-cli/internal/storage/sandboxstate"
	core "github.com/cautem/quarshell-core"
	"github.com/cautem/quarshell-core/defaults"
	"github.com/cautem/quarshell-driver/driver"
	"github.com/cautem/quarshell-runtime/sandbox"
	"github.com/cautem/quarshell-sdk/go/cautem"
)

func TestExecAppliesRequestedTimeoutToDriverContext(t *testing.T) {
	isolateGatewayConfig(t)
	compute := &preflightComputeDriver{}
	app := &App{Sandboxes: &sandbox.Manager{Driver: compute}}
	if err := app.Exec(ExecOpts{Name: "demo", Argv: []string{"sleep", "10"}, Timeout: 5 * time.Second, Stdin: strings.NewReader("input\n")}); err != nil {
		t.Fatal(err)
	}
	if compute.execCalls != 1 || !compute.execDeadline {
		t.Fatalf("execCalls=%d execDeadline=%v; want one call with a deadline", compute.execCalls, compute.execDeadline)
	}
	if got := compute.execReq.Argv; len(got) != 5 || got[0] != "/bin/sh" || got[1] != "-c" || got[4] != "'sleep' '10'" {
		t.Fatalf("driver shell argv=%v", got)
	}
	if input, err := io.ReadAll(compute.execReq.StdinReader); err != nil || string(input) != "input\n" {
		t.Fatalf("driver stdin data=%q err=%v", input, err)
	}
}

type localTerminalTestDriver struct {
	*preflightComputeDriver
	request driver.ExecTerminalRequest
	session *localTerminalTestSession
}

func (d *localTerminalTestDriver) OpenExecTerminal(_ context.Context, id core.ID, request driver.ExecTerminalRequest) (driver.ExecTerminalSession, error) {
	d.inspectName = string(id)
	d.request = request
	return d.session, nil
}

func (*localTerminalTestDriver) BackendCapabilityInfo() driver.BackendInfo {
	return driver.BackendInfo{ID: "vm", Capabilities: map[driver.Capability]struct{}{driver.CapabilityExecTerminal: {}}}
}

type localTerminalTestSession struct {
	output  *strings.Reader
	input   strings.Builder
	sizes   [][2]uint32
	eof     chan struct{}
	eofOnce sync.Once
	code    int
}

func (s *localTerminalTestSession) Read(payload []byte) (int, error) { return s.output.Read(payload) }
func (s *localTerminalTestSession) Close() error                     { return nil }
func (s *localTerminalTestSession) Write(payload []byte) (int, error) {
	count, err := s.input.Write(payload)
	if string(payload) == "\x04" {
		s.eofOnce.Do(func() { close(s.eof) })
	}
	return count, err
}
func (s *localTerminalTestSession) Resize(_ context.Context, columns, rows uint32) error {
	s.sizes = append(s.sizes, [2]uint32{columns, rows})
	return nil
}
func (s *localTerminalTestSession) Wait(ctx context.Context) (int, error) {
	select {
	case <-s.eof:
		return s.code, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func TestLocalExecUsesFullDuplexTerminalExtensionForTTY(t *testing.T) {
	isolateGatewayConfig(t)
	session := &localTerminalTestSession{output: strings.NewReader("terminal-output"), eof: make(chan struct{}), code: 19}
	compute := &localTerminalTestDriver{preflightComputeDriver: &preflightComputeDriver{}, session: session}
	app := &App{Sandboxes: &sandbox.Manager{Driver: compute}, DriverName: "vm"}
	previousStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	runErr := app.Exec(ExecOpts{
		Name: "demo", Argv: []string{"/bin/sh", "-c", "read value; printf '%s' \"$value\""},
		TTY: true, NoLoginShell: true, WorkDir: "/opt/app", Stdin: strings.NewReader("terminal-input\n"),
	})
	_ = writer.Close()
	os.Stdout = previousStdout
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil || !strings.HasSuffix(string(output), "terminal-output") {
		t.Fatalf("local TTY stdout=%q err=%v", output, readErr)
	}
	var exitErr *ExitError
	if !errors.As(runErr, &exitErr) || exitErr.Code != 19 {
		t.Fatalf("local TTY exec error=%v; want exit code 19", runErr)
	}
	if compute.execCalls != 0 || compute.inspectName != "test-sandbox" {
		t.Fatalf("driver exec calls=%d inspected ID=%q; TTY should use the session extension", compute.execCalls, compute.inspectName)
	}
	if !reflect.DeepEqual(compute.request.Argv, []string{"/bin/sh", "-c", "read value; printf '%s' \"$value\""}) ||
		compute.request.WorkDir != "/opt/app" || !compute.request.NoLoginShell || compute.request.Columns != 80 || compute.request.Rows != 24 {
		t.Fatalf("local TTY request = %+v", compute.request)
	}
	for _, want := range driver.CABundleEnv(defaults.GuestCAFile) {
		found := false
		for _, entry := range compute.request.Env {
			if entry == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("local TTY environment omitted %q", want)
		}
	}
	if got := session.input.String(); got != "terminal-input\n\x04" {
		t.Fatalf("local TTY stdin=%q", got)
	}
}

type fakeGatewayExecClient struct {
	stream  *fakeGatewayExecStream
	session *fakeGatewayInteractiveSession
	got     []string
	ws      string
	name    string
	opt     cautem.SandboxExecOptions
	tty     bool
	cols    uint32
	rows    uint32
	timeout time.Duration
}

func (f *fakeGatewayExecClient) ExecStreamingWithTimeout(_ context.Context, workspace, name string, argv []string, timeout time.Duration, options cautem.SandboxExecOptions) (cautem.SandboxExecStream, error) {
	f.ws, f.name, f.got, f.opt, f.timeout = workspace, name, argv, options, timeout
	return f.stream, nil
}

func (f *fakeGatewayExecClient) ExecInteractiveWithTTYTimeout(_ context.Context, workspace, name string, argv []string, tty bool, cols, rows uint32, timeout time.Duration, options cautem.SandboxExecOptions) (cautem.SandboxInteractiveOutput, error) {
	f.ws, f.name, f.got, f.opt, f.timeout = workspace, name, argv, options, timeout
	f.tty, f.cols, f.rows = tty, cols, rows
	return f.session, nil
}

type fakeGatewayInteractiveSession struct {
	output *strings.Reader
	chunks []*cautem.SandboxExecChunk
	input  strings.Builder
	closed bool
}

func (s *fakeGatewayInteractiveSession) Read(data []byte) (int, error)  { return s.output.Read(data) }
func (s *fakeGatewayInteractiveSession) Write(data []byte) (int, error) { return s.input.Write(data) }
func (*fakeGatewayInteractiveSession) Resize(uint32, uint32) error      { return nil }
func (*fakeGatewayInteractiveSession) ExitCode() (int, error)           { return 7, nil }
func (*fakeGatewayInteractiveSession) Close() error                     { return nil }
func (s *fakeGatewayInteractiveSession) CloseWrite() error              { s.closed = true; return nil }
func (s *fakeGatewayInteractiveSession) NextOutput() (*cautem.SandboxExecChunk, error) {
	if len(s.chunks) > 0 {
		chunk := s.chunks[0]
		s.chunks = s.chunks[1:]
		return chunk, nil
	}
	return nil, io.EOF
}

type resizeRecorder struct{ sizes chan [2]uint32 }

func (r resizeRecorder) Resize(cols, rows uint32) error {
	r.sizes <- [2]uint32{cols, rows}
	return nil
}

func TestWatchResizeEventsForwardsCurrentDimensions(t *testing.T) {
	events := make(chan os.Signal, 1)
	session := resizeRecorder{sizes: make(chan [2]uint32, 1)}
	stop := watchResizeEvents(context.Background(), events, func() (uint32, uint32, error) {
		return 121, 44, nil
	}, session)
	events <- os.Interrupt
	select {
	case size := <-session.sizes:
		if size != [2]uint32{121, 44} {
			t.Fatalf("resize = %v, want [121 44]", size)
		}
	case <-time.After(time.Second):
		t.Fatal("resize event was not forwarded")
	}
	stop()
}

type fakeGatewayExecStream struct {
	chunks []*cautem.SandboxExecChunk
	code   int
}

func TestGatewayExecInteractiveSupportsNonTTYStdin(t *testing.T) {
	oldStdout, oldStderr, oldStdin := os.Stdout, os.Stderr, os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = stderrWriter
	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = stdinReader
	t.Cleanup(func() {
		os.Stdout, os.Stderr, os.Stdin = oldStdout, oldStderr, oldStdin
		_ = stdinReader.Close()
		_ = stdinWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
	})
	session := &fakeGatewayInteractiveSession{output: strings.NewReader("outputwarn"), chunks: []*cautem.SandboxExecChunk{
		{Stream: "stdout", Data: []byte("output")}, {Stream: "stderr", Data: []byte("warn")},
	}}
	client := &fakeGatewayExecClient{session: session}
	code, runErr := runGatewayInteractive(context.Background(), client, "team", ExecOpts{
		Name: "shared", Argv: []string{"cat"}, Stdin: strings.NewReader("input"), TTY: false, Timeout: 5 * time.Second,
	}, cautem.SandboxExecOptions{WorkDir: "/work"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, readErr := io.ReadAll(r)
	_ = r.Close()
	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	stderrOut, stderrErr := io.ReadAll(stderrReader)
	_ = stderrReader.Close()
	if runErr != nil || readErr != nil || stderrErr != nil || code != 7 || string(out) != "output" || string(stderrOut) != "warn" {
		t.Fatalf("interactive result: code=%d err=%v stdout=%q stderr=%q readErr=%v stderrErr=%v", code, runErr, out, stderrOut, readErr, stderrErr)
	}
	if !session.closed || session.input.String() != "input" || client.tty || client.cols != 0 || client.rows != 0 || client.timeout != 5*time.Second {
		t.Fatalf("interactive input/tty/timeout mismatch: closed=%v input=%q tty=%v size=%dx%d timeout=%s", session.closed, session.input.String(), client.tty, client.cols, client.rows, client.timeout)
	}
}

func (s *fakeGatewayExecStream) Next() (*cautem.SandboxExecChunk, error) {
	if len(s.chunks) == 0 {
		return nil, io.EOF
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return chunk, nil
}

func (s *fakeGatewayExecStream) ExitCode() (int, error) { return s.code, nil }
func (*fakeGatewayExecStream) Close() error             { return nil }

func TestGatewayExecStreamPreservesOutputOptionsAndExitCode(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })
	client := &fakeGatewayExecClient{stream: &fakeGatewayExecStream{
		chunks: []*cautem.SandboxExecChunk{{Stream: "stdout", Data: []byte("hello\n")}}, code: 19,
	}}
	code, runErr := runGatewayStream(context.Background(), client, "team", ExecOpts{Name: "shared", Argv: []string{"printf", "hello"}, Timeout: 4 * time.Second}, cautem.SandboxExecOptions{
		WorkDir: "/work", Env: map[string]string{"MODE": "test"}, NoLoginShell: true,
	})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, readErr := io.ReadAll(r)
	_ = r.Close()
	if runErr != nil || readErr != nil || code != 19 || string(out) != "hello\n" {
		t.Fatalf("stream result: code=%d err=%v output=%q readErr=%v", code, runErr, out, readErr)
	}
	if client.ws != "team" || client.name != "shared" || strings.Join(client.got, " ") != "printf hello" || client.opt.WorkDir != "/work" || client.opt.Env["MODE"] != "test" || !client.opt.NoLoginShell || client.timeout != 4*time.Second {
		t.Fatalf("stream request not preserved: %+v", client)
	}
}

func TestExecResolvesAndRemembersLastUsedSandbox(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := sandboxstate.SetLast("local:docker", "team", "remembered"); err != nil {
		t.Fatal(err)
	}
	driver := &preflightComputeDriver{}
	app := &App{Sandboxes: &sandbox.Manager{Driver: driver}, DriverName: "docker", GlobalWorkspace: "team"}
	if err := app.Exec(ExecOpts{Argv: []string{"true"}}); err != nil {
		t.Fatal(err)
	}
	if driver.execCalls != 1 || driver.inspectName != "remembered" {
		t.Fatalf("exec calls=%d inspected=%q; want remembered sandbox", driver.execCalls, driver.inspectName)
	}
	last, err := sandboxstate.GetLast("local:docker", "team")
	if err != nil || last != "remembered" {
		t.Fatalf("last sandbox=%q err=%v", last, err)
	}
}

func TestLogsResolvesLastUsedSandbox(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := sandboxstate.SetLast("local:docker", "team", "remembered"); err != nil {
		t.Fatal(err)
	}
	driver := &preflightComputeDriver{}
	app := &App{Sandboxes: &sandbox.Manager{Driver: driver}, DriverName: "docker", GlobalWorkspace: "team"}
	if err := app.LogsToOpts(nil, LogsOpts{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if driver.inspectName != "remembered" || driver.logsID != "test-sandbox" {
		t.Fatalf("logs target: inspect=%q id=%q", driver.inspectName, driver.logsID)
	}
}

func TestLogsPassesTailAndFiltersToOptionDriver(t *testing.T) {
	isolateGatewayConfig(t)
	compute := &preflightComputeDriver{}
	app := &App{Sandboxes: &sandbox.Manager{Driver: compute}}
	err := app.LogsToOpts(context.Background(), LogsOpts{
		Names: []string{"demo"}, Follow: true, Lines: 37, LinesSet: true,
		Since: "15m", Source: "proxy",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !compute.logOptionsUsed || compute.logOptions != (driver.LogOptions{Follow: true, Tail: 37, TailSet: true, Since: "15m", Source: "proxy"}) {
		t.Fatalf("driver log options = %+v, used=%v", compute.logOptions, compute.logOptionsUsed)
	}
}

func TestGatewayLogSourceTreatsAllAsUnfiltered(t *testing.T) {
	if got := gatewayLogSource("all"); got != "" {
		t.Fatalf("gateway all-source filter=%q; want empty filter", got)
	}
	if got := gatewayLogSource("proxy"); got != "proxy" {
		t.Fatalf("gateway proxy-source filter=%q", got)
	}
}

func TestExecZeroTimeoutDoesNotAddDeadline(t *testing.T) {
	isolateGatewayConfig(t)
	driver := &preflightComputeDriver{}
	app := &App{Sandboxes: &sandbox.Manager{Driver: driver}}
	if err := app.Exec(ExecOpts{Name: "demo", Argv: []string{"true"}}); err != nil {
		t.Fatal(err)
	}
	if driver.execCalls != 1 || driver.execDeadline {
		t.Fatalf("execCalls=%d execDeadline=%v; want one call without a deadline", driver.execCalls, driver.execDeadline)
	}
}

func TestExecPreservesRemoteExitCode(t *testing.T) {
	isolateGatewayConfig(t)
	driver := &preflightComputeDriver{execExitCode: 23}
	app := &App{Sandboxes: &sandbox.Manager{Driver: driver}}
	err := app.Exec(ExecOpts{Name: "demo", Argv: []string{"sh", "-c", "exit 23"}})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 23 {
		t.Fatalf("exec error=%v; want remote exit code 23", err)
	}
}

func isolateGatewayConfig(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENSHELL_GATEWAY", "")
	t.Setenv("CAUTEM_GATEWAY_URL", "")
}

func TestExitCodeFindsWrappedRemoteExitError(t *testing.T) {
	err := fmt.Errorf("sandbox command: %w", &ExitError{Code: 23})
	if code, ok := ExitCode(err); !ok || code != 23 {
		t.Fatalf("ExitCode(%v)=(%d,%v); want (23,true)", err, code, ok)
	}
}
