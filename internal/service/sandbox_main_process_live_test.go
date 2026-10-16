package service

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cautem/cautem-driver/driver"
)

// This acceptance test runs the public create path against a configured local
// Engine. It proves argv is the container's canonical process and its exit
// status reaches the CLI result, rather than being launched as a second exec.
func TestSandboxCreateRunsArgvAsCanonicalMainProcess(t *testing.T) {
	image := strings.TrimSpace(os.Getenv("CAUTEM_TEST_MAIN_PROCESS_IMAGE"))
	if image == "" {
		t.Skip("set CAUTEM_TEST_MAIN_PROCESS_IMAGE for live create-main-process acceptance")
	}
	isolateGatewayConfig(t)
	t.Setenv("CAUTEM_DRIVER", "docker")
	t.Setenv("CAUTEM_MAIN_PROCESS_RESTART_POLICY", "")
	app := New()
	if app.Sandboxes == nil || app.Sandboxes.Driver == nil {
		t.Fatal("configured Docker driver is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	app.cmdCtx = ctx
	previousStdout := os.Stdout
	defer func() { os.Stdout = previousStdout }()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	err = app.SandboxCreate(SandboxCreateOpts{
		Name:  "canonical-main-" + time.Now().Format("150405.000000000"),
		Image: image, Workspace: t.TempDir(), NoProxy: true, NoHarden: true, NoVolume: true,
		Argv: []string{"sh", "-c", "echo CAUTEM_CANONICAL_MAIN; exit 19"}, NoKeep: true,
	})
	_ = writer.Close()
	os.Stdout = previousStdout
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatalf("read create attachment output: %v", readErr)
	}
	if !strings.Contains(string(output), "CAUTEM_CANONICAL_MAIN") {
		t.Fatalf("create attachment output=%q; main process output missing", output)
	}
	if code, ok := ExitCode(err); !ok || code != 19 {
		t.Fatalf("sandbox create error=%v; exit=(%d,%v), want canonical exit 19", err, code, ok)
	}
	if _, ok := app.Sandboxes.Driver.(driver.MainProcessWaiter); !ok {
		t.Fatal("Docker driver does not expose canonical main-process wait capability")
	}
}

func TestSandboxCreateAttachesCanonicalMainProcessTTY(t *testing.T) {
	image := strings.TrimSpace(os.Getenv("CAUTEM_TEST_MAIN_PROCESS_IMAGE"))
	if image == "" {
		t.Skip("set CAUTEM_TEST_MAIN_PROCESS_IMAGE for live create-main-process TTY acceptance")
	}
	isolateGatewayConfig(t)
	t.Setenv("CAUTEM_DRIVER", "docker")
	t.Setenv("CAUTEM_MAIN_PROCESS_RESTART_POLICY", "")
	app := New()
	if app.Sandboxes == nil || app.Sandboxes.Driver == nil {
		t.Fatal("configured Docker driver is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	app.cmdCtx = ctx

	previousStdin, previousStdout := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = previousStdin, previousStdout }()
	input, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(inputWriter, "cautem-tty-input\n"); err != nil {
		t.Fatal(err)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	output, outputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin, os.Stdout = input, outputWriter
	err = app.SandboxCreate(SandboxCreateOpts{
		Name:  "canonical-tty-" + time.Now().Format("150405.000000000"),
		Image: image, Workspace: t.TempDir(), NoProxy: true, NoHarden: true, NoVolume: true,
		ForceTTY: true, Argv: []string{"sh", "-c", "read value; printf 'CAUTEM_TTY_REPLY:%s\\n' \"$value\"; exit 7"}, NoKeep: true,
	})
	_ = outputWriter.Close()
	os.Stdin, os.Stdout = previousStdin, previousStdout
	_ = input.Close()
	outputBytes, readErr := io.ReadAll(output)
	_ = output.Close()
	if readErr != nil {
		t.Fatalf("read TTY create output: %v", readErr)
	}
	if !strings.Contains(string(outputBytes), "CAUTEM_TTY_REPLY:cautem-tty-input") {
		t.Fatalf("TTY create output=%q; expected response from canonical process", outputBytes)
	}
	if code, ok := ExitCode(err); !ok || code != 7 {
		t.Fatalf("TTY sandbox create error=%v; exit=(%d,%v), want canonical exit 7", err, code, ok)
	}
}

func TestSandboxAttachReconnectsToDetachedCanonicalMainProcessOnDocker(t *testing.T) {
	image := strings.TrimSpace(os.Getenv("CAUTEM_TEST_MAIN_PROCESS_IMAGE"))
	if image == "" {
		t.Skip("set CAUTEM_TEST_MAIN_PROCESS_IMAGE for live detached-main-process reconnect acceptance")
	}
	isolateGatewayConfig(t)
	t.Setenv("CAUTEM_DRIVER", "docker")
	t.Setenv("CAUTEM_MAIN_PROCESS_RESTART_POLICY", "")
	app := New()
	if app.Sandboxes == nil || app.Sandboxes.Driver == nil {
		t.Fatal("configured Docker driver is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	app.SetCommandContext(ctx)
	name := "canonical-reconnect-" + time.Now().Format("150405000000000")
	defer func() { _ = app.Sandboxes.Remove(context.Background(), name) }()

	previousStdout := os.Stdout
	defer func() { os.Stdout = previousStdout }()
	createOutput, createWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = createWriter
	createErr := app.SandboxCreate(SandboxCreateOpts{
		Name: name, Image: image, Workspace: t.TempDir(), NoProxy: true, NoHarden: true, NoVolume: true,
		Detach: true, Argv: []string{"sh", "-c", "echo CAUTEM_RECONNECT_STARTED; sleep 8; echo CAUTEM_RECONNECT_FINISHED; exit 19"},
	})
	_ = createWriter.Close()
	os.Stdout = previousStdout
	_, _ = io.ReadAll(createOutput)
	_ = createOutput.Close()
	if createErr != nil {
		t.Fatalf("detached canonical create: %v", createErr)
	}

	attachOutput, attachWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = attachWriter
	attachErr := app.SandboxAttach(name, false)
	_ = attachWriter.Close()
	os.Stdout = previousStdout
	output, readErr := io.ReadAll(attachOutput)
	_ = attachOutput.Close()
	if readErr != nil {
		t.Fatalf("read reconnect stream: %v", readErr)
	}
	if !strings.Contains(string(output), "CAUTEM_RECONNECT_STARTED") || !strings.Contains(string(output), "CAUTEM_RECONNECT_FINISHED") {
		t.Fatalf("reconnected output=%q attach error=%v, want canonical process output before and after reconnect", output, attachErr)
	}
	if code, ok := ExitCode(attachErr); !ok || code != 19 {
		t.Fatalf("SandboxAttach() error=%v, exit=(%d,%v); want canonical exit 19", attachErr, code, ok)
	}
}

func TestSandboxAttachReconnectsToDetachedCanonicalTTYOnDocker(t *testing.T) {
	image := strings.TrimSpace(os.Getenv("CAUTEM_TEST_MAIN_PROCESS_IMAGE"))
	if image == "" {
		t.Skip("set CAUTEM_TEST_MAIN_PROCESS_IMAGE for live detached-main-process TTY reconnect acceptance")
	}
	isolateGatewayConfig(t)
	t.Setenv("CAUTEM_DRIVER", "docker")
	t.Setenv("CAUTEM_MAIN_PROCESS_RESTART_POLICY", "")
	app := New()
	if app.Sandboxes == nil || app.Sandboxes.Driver == nil {
		t.Fatal("configured Docker driver is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	app.SetCommandContext(ctx)
	name := "canonical-tty-reconnect-" + time.Now().Format("150405000000000")
	defer func() { _ = app.Sandboxes.Remove(context.Background(), name) }()

	previousStdin, previousStdout := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = previousStdin, previousStdout }()
	createOutput, createWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = createWriter
	createErr := app.SandboxCreate(SandboxCreateOpts{
		Name: name, Image: image, Workspace: t.TempDir(), NoProxy: true, NoHarden: true, NoVolume: true,
		ForceTTY: true, Detach: true, Argv: []string{"sh", "-c", "read value; printf 'CAUTEM_TTY_RECONNECT:%s\\n' \"$value\"; exit 7"},
	})
	_ = createWriter.Close()
	os.Stdout = previousStdout
	_, _ = io.ReadAll(createOutput)
	_ = createOutput.Close()
	if createErr != nil {
		t.Fatalf("detached canonical TTY create: %v", createErr)
	}

	input, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(inputWriter, "reattached-input\n"); err != nil {
		t.Fatal(err)
	}
	if err := inputWriter.Close(); err != nil {
		t.Fatal(err)
	}
	output, outputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin, os.Stdout = input, outputWriter
	attachErr := app.SandboxAttach(name, true)
	_ = outputWriter.Close()
	os.Stdin, os.Stdout = previousStdin, previousStdout
	_ = input.Close()
	outputBytes, readErr := io.ReadAll(output)
	_ = output.Close()
	if readErr != nil {
		t.Fatalf("read TTY reconnect stream: %v", readErr)
	}
	if !strings.Contains(string(outputBytes), "CAUTEM_TTY_RECONNECT:reattached-input") {
		t.Fatalf("reconnected TTY output=%q, want response to reattached input", outputBytes)
	}
	if code, ok := ExitCode(attachErr); !ok || code != 7 {
		t.Fatalf("SandboxAttach(--tty) error=%v, exit=(%d,%v); want canonical exit 7", attachErr, code, ok)
	}
}
