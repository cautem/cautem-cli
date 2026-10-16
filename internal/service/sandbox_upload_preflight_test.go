package service

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	core "github.com/cautem/quarshell-core"
	"github.com/cautem/quarshell-driver/driver"
	"github.com/cautem/quarshell-runtime/sandbox"
)

func TestSandboxUploadSendsGitFilteredTreeToDriver(t *testing.T) {
	isolateGatewayConfig(t)
	repo := t.TempDir()
	gitTestCommand(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"included.txt": "included", "ignored.txt": "ignored"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	compute := &preflightComputeDriver{}
	app := &App{Sandboxes: &sandbox.Manager{Driver: compute}}
	if err := app.SandboxUpload(repo, "demo:/workspace", false); err != nil {
		t.Fatal(err)
	}
	if !containsString(compute.copyEntries, ".gitignore") || !containsString(compute.copyEntries, "included.txt") || containsString(compute.copyEntries, "ignored.txt") {
		t.Fatalf("Git-filtered driver entries=%v; want .gitignore and included.txt only", compute.copyEntries)
	}

	compute.copyEntries = nil
	if err := app.SandboxUpload(repo, "demo:/workspace", true); err != nil {
		t.Fatal(err)
	}
	if !containsString(compute.copyEntries, "ignored.txt") {
		t.Fatalf("unfiltered driver entries=%v; want ignored.txt", compute.copyEntries)
	}
}

func TestSandboxUploadCanRetryAfterPartialDriverFailure(t *testing.T) {
	isolateGatewayConfig(t)
	repo := t.TempDir()
	gitTestCommand(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{"included.txt": "complete payload", "ignored.txt": "must stay ignored"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	compute := &preflightComputeDriver{copyToFailures: 1}
	app := &App{Sandboxes: &sandbox.Manager{Driver: compute}}

	err := app.SandboxUpload(repo, "demo:/workspace", false)
	if err == nil || !strings.Contains(err.Error(), "simulated partial copy") {
		t.Fatalf("first upload error=%v; want simulated partial transfer failure", err)
	}
	if err := app.SandboxUpload(repo, "demo:/workspace", false); err != nil {
		t.Fatalf("retry upload: %v", err)
	}
	if len(compute.copyAttempts) != 2 {
		t.Fatalf("copy attempts=%d; want first attempt plus retry", len(compute.copyAttempts))
	}
	for attempt, entries := range compute.copyAttempts {
		if !containsString(entries, "included.txt") || !containsString(entries, ".gitignore") || containsString(entries, "ignored.txt") {
			t.Fatalf("attempt %d entries=%v; retry must repeat complete Git selection", attempt+1, entries)
		}
	}
	if got, err := os.ReadFile(filepath.Join(repo, "included.txt")); err != nil || string(got) != "complete payload" {
		t.Fatalf("source changed during retry: content=%q err=%v", got, err)
	}
}

func TestSandboxCreateRejectsEmptyGitUploadBeforeProvisioning(t *testing.T) {
	repo := t.TempDir()
	gitTestCommand(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("src/*.ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(repo, "src")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.ignored"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	compute := &preflightComputeDriver{}
	app := &App{Sandboxes: &sandbox.Manager{Driver: compute}}

	err := app.SandboxCreate(SandboxCreateOpts{Upload: source, NoProxy: true, NoHarden: true})
	if err == nil || !strings.Contains(err.Error(), "selected no files") {
		t.Fatalf("SandboxCreate() error=%v; want Git preflight rejection", err)
	}
	if compute.createCalls != 0 {
		t.Fatalf("driver Create() called %d times after rejected upload; want 0", compute.createCalls)
	}
}

func TestSandboxCreateUploadDestinationMatchesSourceType(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "source-link")
	if err := os.Symlink("source.txt", link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	for _, test := range []struct {
		name, staged, original, want string
	}{
		{name: "directory", staged: dir, original: dir, want: "/workspace"},
		{name: "file", staged: file, original: file, want: "/workspace/source.txt"},
		{name: "symlink", staged: link, original: link, want: "/workspace/source-link"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := sandboxCreateUploadDestination(test.staged, test.original)
			if err != nil || got != test.want {
				t.Fatalf("sandboxCreateUploadDestination()=(%q,%v); want %q", got, err, test.want)
			}
		})
	}
}

func gitTestCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

type preflightComputeDriver struct {
	createCalls    int
	copyEntries    []string
	copyAttempts   [][]string
	copyToFailures int
	copyFromCalls  int
	execCalls      int
	execReq        driver.ExecRequest
	execDeadline   bool
	execExitCode   int
	inspectName    string
	logsID         core.ID
	logsFollow     bool
	logOptions     driver.LogOptions
	logOptionsUsed bool
	mainWaitCode   int
	mainWaitErr    error
	mainLogsID     core.ID
	inspectStatus  string
	createSpec     driver.Spec
}

func (d *preflightComputeDriver) Create(_ context.Context, spec driver.Spec) (driver.Handle, error) {
	d.createCalls++
	d.createSpec = spec
	return driver.Handle{}, nil
}
func (*preflightComputeDriver) Start(context.Context, core.ID) error { return nil }
func (*preflightComputeDriver) Stop(context.Context, core.ID) error  { return nil }
func (d *preflightComputeDriver) Exec(ctx context.Context, _ core.ID, req driver.ExecRequest) (driver.ExecResult, error) {
	d.execCalls++
	d.execReq = req
	_, d.execDeadline = ctx.Deadline()
	return driver.ExecResult{ExitCode: d.execExitCode}, nil
}
func (*preflightComputeDriver) Delete(context.Context, core.ID) error { return nil }
func (*preflightComputeDriver) List(context.Context) ([]driver.Info, error) {
	return nil, nil
}
func (d *preflightComputeDriver) Inspect(_ context.Context, name string) (driver.Info, error) {
	d.inspectName = name
	status := d.inspectStatus
	if status == "" {
		status = "running"
	}
	return driver.Info{ID: core.ID("test-sandbox"), Name: name, Status: status}, nil
}
func (d *preflightComputeDriver) Logs(_ context.Context, id core.ID, follow bool, _ io.Writer) error {
	d.logsID = id
	d.logsFollow = follow
	return nil
}
func (d *preflightComputeDriver) LogsWithOptions(_ context.Context, id core.ID, opts driver.LogOptions, _ io.Writer) error {
	d.logsID = id
	d.logsFollow = opts.Follow
	d.logOptions = opts
	d.logOptionsUsed = true
	return nil
}
func (d *preflightComputeDriver) WaitMainProcess(context.Context, core.ID) (int, error) {
	return d.mainWaitCode, d.mainWaitErr
}
func (d *preflightComputeDriver) OpenMainProcessLogs(_ context.Context, id core.ID) (io.ReadCloser, error) {
	d.mainLogsID = id
	return io.NopCloser(strings.NewReader("canonical-output\n")), nil
}

func (d *preflightComputeDriver) CopyTo(_ context.Context, _ core.ID, source, _ string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	d.copyEntries = d.copyEntries[:0]
	for _, entry := range entries {
		d.copyEntries = append(d.copyEntries, entry.Name())
	}
	d.copyAttempts = append(d.copyAttempts, append([]string(nil), d.copyEntries...))
	if d.copyToFailures > 0 {
		d.copyToFailures--
		return errors.New("simulated partial copy failure")
	}
	return nil
}
func (d *preflightComputeDriver) CopyFrom(context.Context, core.ID, string, string) error {
	d.copyFromCalls++
	return nil
}
func (*preflightComputeDriver) EnsureSSHDaemon(context.Context, core.ID) error { return nil }
