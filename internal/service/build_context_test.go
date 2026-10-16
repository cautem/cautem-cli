package service

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cautem/quarshell-driver/driver"
	"github.com/cautem/quarshell-runtime/sandbox"
)

func TestOpenBuildContextAppliesDockerignoreAndKeepsDockerfile(t *testing.T) {
	root := t.TempDir()
	writeBuildFile(t, root, "Dockerfile.dev", "FROM scratch\n")
	writeBuildFile(t, root, ".dockerignore", "secret.txt\nlogs/*\n!logs/keep.txt\n")
	writeBuildFile(t, root, "secret.txt", "secret")
	writeBuildFile(t, root, "logs/drop.txt", "drop")
	writeBuildFile(t, root, "logs/keep.txt", "keep")
	writeBuildFile(t, root, "app.txt", "app")

	archive, dockerfile, err := openBuildContext(context.Background(), root, "Dockerfile.dev")
	if err != nil {
		t.Fatal(err)
	}
	entries := readBuildTar(t, archive.reader)
	if err := archive.CloseAndWait(); err != nil {
		t.Fatal(err)
	}
	if dockerfile != "Dockerfile.dev" {
		t.Fatalf("Dockerfile=%q, want Dockerfile.dev", dockerfile)
	}
	for _, name := range []string{"Dockerfile.dev", ".dockerignore", "app.txt", "logs/keep.txt"} {
		if _, ok := entries[name]; !ok {
			t.Errorf("build context omitted %q", name)
		}
	}
	for _, name := range []string{"secret.txt", "logs/drop.txt"} {
		if _, ok := entries[name]; ok {
			t.Errorf("build context included ignored file %q", name)
		}
	}
}

func TestOpenBuildContextRejectsUnsafeDockerfileAndSymlink(t *testing.T) {
	root := t.TempDir()
	writeBuildFile(t, root, "Dockerfile", "FROM scratch\n")
	for _, dockerfile := range []string{"../Dockerfile", filepath.Join(root, "Dockerfile"), "missing"} {
		if archive, _, err := openBuildContext(context.Background(), root, dockerfile); err == nil {
			_ = archive.CloseAndWait()
			t.Errorf("openBuildContext(..., %q) unexpectedly succeeded", dockerfile)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	archive, _, err := openBuildContext(context.Background(), root, "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(io.Discard, archive.reader)
	closeErr := archive.CloseAndWait()
	if copyErr == nil && closeErr == nil {
		t.Fatal("build context accepted a symlink escaping its root")
	}
}

func TestBuildLocalSourceImageUsesEngineBuilder(t *testing.T) {
	root := t.TempDir()
	writeBuildFile(t, root, "Dockerfile", "FROM scratch\n")
	writeBuildFile(t, root, "app.txt", "payload")
	compute := &imageBuildTestDriver{preflightComputeDriver: &preflightComputeDriver{}}
	var output bytes.Buffer
	tag, err := buildLocalSourceImage(context.Background(), compute, root, "", "demo", &output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tag, "cautem-local/build:") || compute.tag != tag {
		t.Fatalf("build tag=%q, driver tag=%q", tag, compute.tag)
	}
	if compute.dockerfile != "Dockerfile" || !bytes.Contains(compute.context, []byte("app.txt")) || !strings.Contains(output.String(), tag) {
		t.Fatalf("build request Dockerfile=%q context has app=%v output=%q", compute.dockerfile, bytes.Contains(compute.context, []byte("app.txt")), output.String())
	}
}

func TestSandboxCreateBuildsImageBeforePassingSpecToDriver(t *testing.T) {
	root := t.TempDir()
	writeBuildFile(t, root, "containers/Dockerfile", "FROM scratch\n")
	writeBuildFile(t, root, "app.txt", "payload")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()
	compute := &imageBuildTestDriver{preflightComputeDriver: &preflightComputeDriver{}}
	app := &App{Sandboxes: &sandbox.Manager{Driver: compute}}
	err := app.SandboxCreate(SandboxCreateOpts{
		Name: "built-image", BuildContext: root, Dockerfile: "containers/Dockerfile", NoProxy: true,
		NoHarden: true, NoVolume: true, NoCredentialWarnings: true, GatewayURL: gateway.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if compute.createCalls != 1 || compute.createSpec.Image != compute.tag || compute.dockerfile != "containers/Dockerfile" {
		t.Fatalf("driver create calls=%d image=%q build tag=%q Dockerfile=%q", compute.createCalls, compute.createSpec.Image, compute.tag, compute.dockerfile)
	}
}

type imageBuildTestDriver struct {
	*preflightComputeDriver
	tag        string
	dockerfile string
	context    []byte
}

func (d *imageBuildTestDriver) BuildImage(_ context.Context, request driver.ImageBuildRequest) error {
	d.tag, d.dockerfile = request.Tag, request.Dockerfile
	var err error
	d.context, err = io.ReadAll(request.Context)
	return err
}

func writeBuildFile(t *testing.T, root, name, value string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readBuildTar(t *testing.T, input io.Reader) map[string][]byte {
	t.Helper()
	entries := map[string][]byte{}
	reader := tar.NewReader(input)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		entries[strings.TrimSuffix(header.Name, "/")] = data
	}
}
