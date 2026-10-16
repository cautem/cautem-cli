package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cautem/cautem-driver/driver"
	"github.com/cautem/cautem-runtime/sandbox"
)

// Run with CAUTEM_TEST_UPLOAD_IMAGE set and CAUTEM_DRIVER=docker|podman to
// verify Git selection through the configured Engine CopyTo implementation.
func TestSandboxUploadGitFilteringAgainstConfiguredEngine(t *testing.T) {
	image := os.Getenv("CAUTEM_TEST_UPLOAD_IMAGE")
	if image == "" {
		t.Skip("set CAUTEM_TEST_UPLOAD_IMAGE to run the disposable Engine upload acceptance test")
	}
	isolateGatewayConfig(t)

	app := New()
	if app.Sandboxes == nil || app.Sandboxes.Driver == nil {
		t.Fatal("configured sandbox driver is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := "cautem-upload-git-" + time.Now().Format("150405.000000000")
	handle, err := app.Sandboxes.Create(ctx, sandbox.CreateOptions{Spec: driver.Spec{
		Name: name, Image: image, Workspace: os.TempDir(),
		Command: []string{"sh", "-c", "sleep 120"}, NoHarden: true,
	}})
	if err != nil {
		t.Fatalf("create disposable upload sandbox: %v", err)
	}
	name = handle.Name
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = app.Sandboxes.Remove(cleanupCtx, string(handle.ID))
	})

	repo := t.TempDir()
	gitUploadTestCommand(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "included.txt"), []byte("visible"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "ignored.txt"), []byte("excluded"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.SandboxUpload(repo, name+":/workspace", false); err != nil {
		t.Fatalf("Git-aware upload: %v", err)
	}
	remoteTree := filepath.ToSlash(filepath.Join("/workspace", filepath.Base(repo)))
	check := "test \"$(cat " + remoteTree + "/included.txt)\" = visible && test ! -e " + remoteTree + "/ignored.txt"
	result, err := app.Sandboxes.Exec(ctx, name, driver.ExecRequest{Argv: []string{"sh", "-c", check}})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("uploaded Git selection: result=%+v err=%v", result, err)
	}
}

func gitUploadTestCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
