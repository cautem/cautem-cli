package service

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cautem/cautem-sdk/go/cautem"
)

type transferInteractiveSession struct {
	input    bytes.Buffer
	closed   bool
	writeErr error
}

func (s *transferInteractiveSession) Read([]byte) (int, error) { return 0, io.EOF }
func (s *transferInteractiveSession) Write(data []byte) (int, error) {
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return s.input.Write(data)
}
func (s *transferInteractiveSession) Resize(uint32, uint32) error { return nil }
func (s *transferInteractiveSession) ExitCode() (int, error)      { return 0, nil }
func (s *transferInteractiveSession) Close() error                { return nil }
func (s *transferInteractiveSession) CloseWrite() error           { s.closed = true; return nil }
func (s *transferInteractiveSession) NextOutput() (*cautem.SandboxExecChunk, error) {
	return nil, io.EOF
}

type transferExecClient struct {
	session            *transferInteractiveSession
	stream             cautem.SandboxExecStream
	workspace, sandbox string
	argv               []string
	tty                bool
	timeout            time.Duration
	options            cautem.SandboxExecOptions
}

func shellTestPath(t *testing.T, root, target string) string {
	t.Helper()
	relative, err := filepath.Rel(root, target)
	if err != nil {
		t.Fatalf("make shell test path relative: %v", err)
	}
	return filepath.ToSlash(relative)
}

func (c *transferExecClient) ExecStreamingWithTimeout(_ context.Context, workspace, sandbox string, argv []string, timeout time.Duration, options cautem.SandboxExecOptions) (cautem.SandboxExecStream, error) {
	c.workspace, c.sandbox, c.argv, c.timeout, c.options = workspace, sandbox, argv, timeout, options
	if c.stream == nil {
		return nil, errors.New("unexpected streamed exec")
	}
	return c.stream, nil
}

func (c *transferExecClient) ExecInteractiveWithTTYTimeout(_ context.Context, workspace, sandbox string, argv []string, tty bool, _, _ uint32, timeout time.Duration, options cautem.SandboxExecOptions) (cautem.SandboxInteractiveOutput, error) {
	c.workspace, c.sandbox, c.argv, c.tty, c.timeout, c.options = workspace, sandbox, argv, tty, timeout, options
	return c.session, nil
}

func TestMakeUploadArchivePreservesDirectoryContentsAndSymlinks(t *testing.T) {
	source := filepath.Join(t.TempDir(), "project tree")
	if err := os.Mkdir(source, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "nested"), 0o710); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(source, "nested", "hello.txt")
	if err := os.WriteFile(file, []byte("payload"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("nested", "hello.txt"), filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}

	archive, kind, base, err := makeUploadArchive(source)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if kind != "directory" || base != "project tree" {
		t.Fatalf("kind/base = %q/%q", kind, base)
	}
	r := tar.NewReader(archive)
	seen := map[string]string{}
	for {
		header, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeSymlink {
			seen[header.Name] = "symlink:" + header.Linkname
			continue
		}
		if header.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			seen[header.Name] = string(data)
		} else {
			seen[header.Name] = "directory"
		}
	}
	if seen["project tree"] != "directory" || seen["project tree/nested/hello.txt"] != "payload" || seen["project tree/link"] != "symlink:nested/hello.txt" {
		t.Fatalf("archive entries = %#v", seen)
	}
}

func TestUploadGatewayPathStreamsArchiveOverNonTTYExec(t *testing.T) {
	source := filepath.Join(t.TempDir(), "report file.txt")
	if err := os.WriteFile(source, []byte("gateway payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := &transferInteractiveSession{}
	client := &transferExecClient{session: session}
	timeout := 23 * time.Second
	if err := uploadGatewayPath(context.Background(), client, "team-a", "shared", source, "/workspace/reports/renamed.txt", timeout); err != nil {
		t.Fatal(err)
	}
	if client.workspace != "team-a" || client.sandbox != "shared" || client.tty || client.timeout != timeout || !client.options.NoLoginShell {
		t.Fatalf("exec request: workspace=%q sandbox=%q tty=%v timeout=%s options=%+v", client.workspace, client.sandbox, client.tty, client.timeout, client.options)
	}
	if len(client.argv) != 7 || client.argv[0] != "sh" || client.argv[1] != "-c" || client.argv[3] != "cautem-upload" || client.argv[4] != "/workspace/reports/renamed.txt" || client.argv[5] != "file" || client.argv[6] != "report file.txt" {
		t.Fatalf("exec argv = %#v", client.argv)
	}
	if !session.closed {
		t.Fatal("upload did not close remote stdin")
	}
	r := tar.NewReader(bytes.NewReader(session.input.Bytes()))
	header, err := r.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Name != "report file.txt" {
		t.Fatalf("archive entry = %q", header.Name)
	}
	content, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "gateway payload" {
		t.Fatalf("archive content = %q", content)
	}
}

func TestUploadGatewayPathReturnsInterruptedInputError(t *testing.T) {
	source := filepath.Join(t.TempDir(), "interrupted.txt")
	if err := os.WriteFile(source, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := &transferInteractiveSession{writeErr: errors.New("stream interrupted")}
	client := &transferExecClient{session: session}
	err := uploadGatewayPath(context.Background(), client, "default", "shared", source, "/workspace/interrupted.txt", time.Second)
	if err == nil || !strings.Contains(err.Error(), "stream interrupted") {
		t.Fatalf("upload error=%v; want interrupted stream error", err)
	}
	if !session.closed {
		t.Fatal("upload did not half-close remote input after interruption")
	}
}

func TestGatewayUploadScriptExtractsFilesAndChecksWorkspaceBoundary(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "out")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "main.go"), []byte("package main"), 0o640); err != nil {
		t.Fatal(err)
	}
	archive, kind, base, err := makeUploadArchive(project)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", gatewayUploadScript, "cautem-upload", shellTestPath(t, root, destination), kind, base)
	cmd.Dir = root
	cmd.Stdin = archive
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("directory upload script: %v: %s", err, output)
	}
	if got, err := os.ReadFile(filepath.Join(destination, "project", "main.go")); err != nil || string(got) != "package main" {
		t.Fatalf("directory upload contents=%q err=%v", got, err)
	}
	existingDirectory := filepath.Join(root, "existing-directory-upload")
	if err := os.Mkdir(existingDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("sh", "-c", gatewayUploadScript, "cautem-upload", shellTestPath(t, root, existingDirectory), kind, base)
	cmd.Dir, cmd.Stdin = root, archive
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("directory upload into existing directory: %v: %s", err, output)
	}
	if got, err := os.ReadFile(filepath.Join(existingDirectory, "project", "main.go")); err != nil || string(got) != "package main" {
		t.Fatalf("directory upload into existing directory contents=%q err=%v", got, err)
	}

	file := filepath.Join(t.TempDir(), "original.txt")
	if err := os.WriteFile(file, []byte("renamed"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileArchive, fileKind, fileBase, err := makeUploadArchive(file)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(fileArchive.Name())
	defer fileArchive.Close()
	if _, err := fileArchive.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "renamed.txt")
	cmd = exec.Command("sh", "-c", gatewayUploadScript, "cautem-upload", shellTestPath(t, root, target), fileKind, fileBase)
	cmd.Dir, cmd.Stdin = root, fileArchive
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("file upload script: %v: %s", err, output)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "renamed" {
		t.Fatalf("renamed file contents=%q err=%v", got, err)
	}

	if _, err := fileArchive.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	trailingSlashDir := filepath.Join(root, "created-by-trailing-slash") + "/"
	cmd = exec.Command("sh", "-c", gatewayUploadScript, "cautem-upload", shellTestPath(t, root, trailingSlashDir)+"/", fileKind, fileBase)
	cmd.Dir, cmd.Stdin = root, fileArchive
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("trailing slash upload: %v: %s", err, output)
	}
	if got, err := os.ReadFile(filepath.Join(root, "created-by-trailing-slash", "original.txt")); err != nil || string(got) != "renamed" {
		t.Fatalf("trailing slash upload contents=%q err=%v", got, err)
	}

	if _, err := fileArchive.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	existingDir := filepath.Join(root, "existing-directory")
	if err := os.Mkdir(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("sh", "-c", gatewayUploadScript, "cautem-upload", shellTestPath(t, root, existingDir), fileKind, fileBase)
	cmd.Dir, cmd.Stdin = root, fileArchive
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("existing directory upload: %v: %s", err, output)
	}
	if got, err := os.ReadFile(filepath.Join(existingDir, "original.txt")); err != nil || string(got) != "renamed" {
		t.Fatalf("existing directory upload contents=%q err=%v", got, err)
	}

	if _, err := fileArchive.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	danglingDestination := filepath.Join(root, "dangling-destination")
	if err := os.Symlink("missing-target", danglingDestination); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("sh", "-c", gatewayUploadScript, "cautem-upload", shellTestPath(t, root, danglingDestination), fileKind, fileBase)
	cmd.Dir, cmd.Stdin = root, fileArchive
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dangling symlink upload: %v: %s", err, output)
	}
	info, err := os.Lstat(danglingDestination)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("dangling destination was not replaced by regular file: mode=%v err=%v", info.Mode(), err)
	}

	if _, err := fileArchive.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	escapePath := shellTestPath(t, root, filepath.Join(root, "..", "outside.txt"))
	escape := exec.Command("sh", "-c", gatewayUploadScript, "cautem-upload", escapePath, fileKind, fileBase)
	escape.Dir, escape.Stdin = root, fileArchive
	output, err := escape.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "escapes sandbox workspace") {
		t.Fatalf("workspace escape result: err=%v output=%q", err, output)
	}
}

func TestGatewayUploadDirectoryPartialTransferCanBeRetried(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "workspace")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "new.txt"), bytes.Repeat([]byte("payload"), 1024), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(destination, "project")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "existing.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	archive, kind, base, err := makeUploadArchive(project)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) < 2048 {
		t.Fatalf("upload archive unexpectedly small: %d bytes", len(contents))
	}

	partial := exec.Command("sh", "-c", gatewayUploadScript, "cautem-upload", shellTestPath(t, root, destination), kind, base)
	partial.Dir = root
	partial.Stdin = bytes.NewReader(contents[:2048])
	if output, err := partial.CombinedOutput(); err == nil {
		t.Fatalf("partial directory upload succeeded: %s", output)
	}
	if got, err := os.ReadFile(filepath.Join(target, "existing.txt")); err != nil || string(got) != "keep" {
		t.Fatalf("existing file after interrupted transfer=%q err=%v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(target, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("partial upload was published before retry: %v", err)
	}

	retry := exec.Command("sh", "-c", gatewayUploadScript, "cautem-upload", shellTestPath(t, root, destination), kind, base)
	retry.Dir = root
	retry.Stdin = bytes.NewReader(contents)
	if output, err := retry.CombinedOutput(); err != nil {
		t.Fatalf("retry directory upload: %v: %s", err, output)
	}
	got, err := os.ReadFile(filepath.Join(target, "new.txt"))
	if err != nil || !bytes.Equal(got, bytes.Repeat([]byte("payload"), 1024)) {
		t.Fatalf("retried upload content length=%d err=%v", len(got), err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "existing.txt")); err != nil || string(got) != "keep" {
		t.Fatalf("existing file after retry=%q err=%v", got, err)
	}
}

func TestDownloadGatewayPathStreamsAndRenamesFile(t *testing.T) {
	archive := makeTarFixture(t, []tarFixtureEntry{{name: "report.txt", kind: tar.TypeReg, body: "gateway report", mode: 0o640}})
	stream := &fakeGatewayExecStream{chunks: []*cautem.SandboxExecChunk{
		{Stream: "stdout", Data: archive[:11]},
		{Stream: "stdout", Data: archive[11:]},
	}, code: 0}
	client := &transferExecClient{stream: stream}
	destination := filepath.Join(t.TempDir(), "nested", "renamed.txt")
	timeout := 19 * time.Second
	if err := downloadGatewayPath(context.Background(), client, "team-b", "shared", "/workspace/reports/report.txt", destination, timeout); err != nil {
		t.Fatal(err)
	}
	if client.workspace != "team-b" || client.sandbox != "shared" || client.timeout != timeout || !client.options.NoLoginShell {
		t.Fatalf("exec request workspace=%q sandbox=%q timeout=%s options=%+v", client.workspace, client.sandbox, client.timeout, client.options)
	}
	if len(client.argv) != 5 || client.argv[0] != "sh" || client.argv[1] != "-c" || client.argv[3] != "cautem-download" || client.argv[4] != "/workspace/reports/report.txt" {
		t.Fatalf("exec argv=%#v", client.argv)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != "gateway report" {
		t.Fatalf("download content=%q err=%v", got, err)
	}
	info, err := os.Stat(destination)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o640) {
		t.Fatalf("download mode=%v err=%v", info.Mode().Perm(), err)
	}
}

func TestDownloadGatewayPathHandlesMultiMegabyteStream(t *testing.T) {
	body := bytes.Repeat([]byte("large-transfer-check\n"), 450_000)
	archive := makeTarFixture(t, []tarFixtureEntry{{name: "large.bin", kind: tar.TypeReg, body: string(body), mode: 0o600}})
	stream := &fakeGatewayExecStream{chunks: []*cautem.SandboxExecChunk{
		{Stream: "stdout", Data: archive[:len(archive)/2]},
		{Stream: "stdout", Data: archive[len(archive)/2:]},
	}, code: 0}
	client := &transferExecClient{stream: stream}
	destination := filepath.Join(t.TempDir(), "large.bin")
	if err := downloadGatewayPath(context.Background(), client, "default", "shared", "/workspace/large.bin", destination, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || len(got) != len(body) || !bytes.Equal(got[:64], body[:64]) || !bytes.Equal(got[len(got)-64:], body[len(body)-64:]) {
		t.Fatalf("large download len=%d want=%d err=%v", len(got), len(body), err)
	}
}

func TestDownloadGatewayPathExtractsDirectoryAndSymlink(t *testing.T) {
	archive := makeTarFixture(t, []tarFixtureEntry{
		{name: "tree/", kind: tar.TypeDir, mode: 0o750},
		{name: "tree/nested/", kind: tar.TypeDir, mode: 0o710},
		{name: "tree/nested/file.txt", kind: tar.TypeReg, body: "contents", mode: 0o600},
		{name: "tree/file-link", kind: tar.TypeSymlink, link: "nested/file.txt", mode: 0o777},
	})
	client := &transferExecClient{stream: &fakeGatewayExecStream{chunks: []*cautem.SandboxExecChunk{{Stream: "stdout", Data: archive}}, code: 0}}
	destination := filepath.Join(t.TempDir(), "downloaded-tree")
	if err := downloadGatewayPath(context.Background(), client, "default", "shared", "/workspace/tree", destination, time.Second); err != nil {
		t.Fatal(err)
	}
	downloadedTree := filepath.Join(destination, "tree")
	got, err := os.ReadFile(filepath.Join(downloadedTree, "nested", "file.txt"))
	if err != nil || string(got) != "contents" {
		t.Fatalf("directory file=%q err=%v", got, err)
	}
	info, err := os.Stat(filepath.Join(downloadedTree, "nested"))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o710) {
		t.Fatalf("directory mode=%v err=%v", info.Mode().Perm(), err)
	}
	info, err = os.Stat(filepath.Join(downloadedTree, "nested", "file.txt"))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("directory file mode=%v err=%v", info.Mode().Perm(), err)
	}
	link, err := os.Readlink(filepath.Join(downloadedTree, "file-link"))
	if err != nil || link != "nested/file.txt" {
		t.Fatalf("directory symlink=%q err=%v", link, err)
	}
}

func TestDownloadGatewayPathUsesExistingDirectoryAndRefusesDirectoryOverFile(t *testing.T) {
	fileArchive := makeTarFixture(t, []tarFixtureEntry{{name: "report.txt", kind: tar.TypeReg, body: "file", mode: 0o600}})
	destinationDir := t.TempDir()
	client := &transferExecClient{stream: &fakeGatewayExecStream{chunks: []*cautem.SandboxExecChunk{{Stream: "stdout", Data: fileArchive}}, code: 0}}
	if err := downloadGatewayPath(context.Background(), client, "default", "shared", "/workspace/report.txt", destinationDir, time.Second); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(destinationDir, "report.txt")); err != nil || string(got) != "file" {
		t.Fatalf("download into existing directory=%q err=%v", got, err)
	}

	directoryArchive := makeTarFixture(t, []tarFixtureEntry{
		{name: "./", kind: tar.TypeDir, mode: 0o755},
		{name: "./nested.txt", kind: tar.TypeReg, body: "directory", mode: 0o600},
	})
	client.stream = &fakeGatewayExecStream{chunks: []*cautem.SandboxExecChunk{{Stream: "stdout", Data: directoryArchive}}, code: 0}
	fileDestination := filepath.Join(t.TempDir(), "existing-file")
	if err := os.WriteFile(fileDestination, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := downloadGatewayPath(context.Background(), client, "default", "shared", "/workspace/tree", fileDestination, time.Second)
	if err == nil || !strings.Contains(err.Error(), "cannot extract directory") {
		t.Fatalf("directory over file error=%v", err)
	}
	if got, err := os.ReadFile(fileDestination); err != nil || string(got) != "keep" {
		t.Fatalf("conflicting destination contents=%q err=%v", got, err)
	}
}

func TestGatewayDownloadScriptRejectsSourceOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", gatewayDownloadScript, "cautem-download", shellTestPath(t, root, outside))
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "escapes workspace") {
		t.Fatalf("source boundary result: err=%v output=%q", err, output)
	}
	link := filepath.Join(root, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("sh", "-c", gatewayDownloadScript, "cautem-download", shellTestPath(t, root, link))
	cmd.Dir = root
	output, err = cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "escapes workspace") {
		t.Fatalf("symlink boundary result: err=%v output=%q", err, output)
	}
}

func TestDownloadGatewayPathDoesNotWriteWhenRemoteCommandFails(t *testing.T) {
	archive := makeTarFixture(t, []tarFixtureEntry{{name: "report.txt", kind: tar.TypeReg, body: "partial", mode: 0o600}})
	client := &transferExecClient{stream: &fakeGatewayExecStream{chunks: []*cautem.SandboxExecChunk{{Stream: "stdout", Data: archive}}, code: 12}}
	destination := filepath.Join(t.TempDir(), "report.txt")
	err := downloadGatewayPath(context.Background(), client, "default", "shared", "/workspace/report.txt", destination, time.Second)
	if err == nil || !strings.Contains(err.Error(), "status 12") {
		t.Fatalf("download error=%v; want remote exit status", err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed remote command left destination behind: stat err=%v", err)
	}
}

func TestDownloadGatewayPathDoesNotWritePartialStream(t *testing.T) {
	archive := makeTarFixture(t, []tarFixtureEntry{{name: "report.txt", kind: tar.TypeReg, body: "partial", mode: 0o600}})
	stream := &interruptedDownloadStream{chunk: &cautem.SandboxExecChunk{Stream: "stdout", Data: archive}, err: errors.New("transport interrupted")}
	client := &transferExecClient{stream: stream}
	destination := filepath.Join(t.TempDir(), "report.txt")
	err := downloadGatewayPath(context.Background(), client, "default", "shared", "/workspace/report.txt", destination, time.Second)
	if err == nil || !strings.Contains(err.Error(), "transport interrupted") {
		t.Fatalf("download error=%v; want interrupted stream error", err)
	}
	if !stream.closed {
		t.Fatal("interrupted stream was not closed")
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted stream left destination behind: stat err=%v", err)
	}
}

func TestExtractGatewayDownloadTarRejectsTraversalAndSymlinkTraversal(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarFixtureEntry
	}{
		{name: "parent traversal", entries: []tarFixtureEntry{
			{name: "./", kind: tar.TypeDir, mode: 0o755},
			{name: "./../escaped", kind: tar.TypeReg, body: "bad", mode: 0o600},
		}},
		{name: "symlink traversal", entries: []tarFixtureEntry{
			{name: "./", kind: tar.TypeDir, mode: 0o755},
			{name: "./pivot", kind: tar.TypeSymlink, link: "../../outside", mode: 0o777},
			{name: "./pivot/payload", kind: tar.TypeReg, body: "bad", mode: 0o600},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, "destination")
			if err := os.Mkdir(destination, 0o755); err != nil {
				t.Fatal(err)
			}
			archiveFile, err := os.CreateTemp(root, "archive-*.tar")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(archiveFile.Name())
			if _, err := archiveFile.Write(makeTarFixture(t, test.entries)); err != nil {
				t.Fatal(err)
			}
			if _, err := archiveFile.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			if err := extractGatewayDownloadTar(archiveFile, destination); err == nil {
				t.Fatal("unsafe archive unexpectedly extracted")
			}
			_ = archiveFile.Close()
			if _, err := os.Lstat(filepath.Join(root, "escaped")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("archive escaped destination: stat err=%v", err)
			}
		})
	}
}

type tarFixtureEntry struct {
	name string
	kind byte
	body string
	link string
	mode int64
}

type interruptedDownloadStream struct {
	chunk  *cautem.SandboxExecChunk
	err    error
	closed bool
}

func (s *interruptedDownloadStream) Next() (*cautem.SandboxExecChunk, error) {
	if s.chunk != nil {
		chunk := s.chunk
		s.chunk = nil
		return chunk, nil
	}
	return nil, s.err
}
func (*interruptedDownloadStream) ExitCode() (int, error) { return 0, nil }
func (s *interruptedDownloadStream) Close() error         { s.closed = true; return nil }

func makeTarFixture(t *testing.T, entries []tarFixtureEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	w := tar.NewWriter(&buffer)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Typeflag: entry.kind, Mode: entry.mode, Linkname: entry.link}
		if entry.kind == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		if err := w.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.kind == tar.TypeReg {
			if _, err := io.WriteString(w, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
