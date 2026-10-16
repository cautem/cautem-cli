package service

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const cliPodmanSandboxImage = "public.ecr.aws/docker/library/alpine:3.22"

func TestPodmanCLIReconnectsToDetachedCanonicalMainProcessAndTTY(t *testing.T) {
	if testing.Short() || os.Getenv("CAUTEM_TESTCONTAINERS") != "1" {
		t.Skip("set CAUTEM_TESTCONTAINERS=1 to run Podman CLI lifecycle acceptance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve Podman workspace path: %v", err)
	}
	image := strings.TrimSpace(os.Getenv("CAUTEM_TEST_PODMAN_IMAGE"))
	if image == "" {
		image = "quay.io/podman/stable:v5.8.7"
	}
	service, err := testcontainers.Run(ctx, image,
		testcontainers.WithExposedPorts("18500/tcp"),
		testcontainers.WithEnv(map[string]string{"STORAGE_DRIVER": "vfs"}),
		testcontainers.WithHostConfigModifier(func(config *container.HostConfig) { config.Privileged = true }),
		testcontainers.WithCmd("sh", "-lc", "printf 'root:100000:65536\\n' > /etc/subuid; printf 'root:100000:65536\\n' > /etc/subgid; while true; do podman system service --time=0 tcp:0.0.0.0:18500 & pid=$!; echo $pid >/tmp/podman-service.pid; wait $pid; sleep 2; done"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("18500/tcp")),
	)
	if err != nil {
		t.Fatalf("start disposable Podman API: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := service.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate disposable Podman API: %v", err)
		}
	})
	if _, _, err := service.Exec(ctx, []string{"mkdir", "-p", "--", workspace}); err != nil {
		t.Fatalf("create workspace in Podman service namespace: %v", err)
	}
	port, err := service.MappedPort(ctx, "18500/tcp")
	if err != nil {
		t.Fatalf("map Podman API port: %v", err)
	}
	host, err := service.Host(ctx)
	if err != nil {
		t.Fatalf("resolve Podman API host: %v", err)
	}

	isolateGatewayConfig(t)
	t.Setenv("CAUTEM_DRIVER", "podman")
	t.Setenv("DOCKER_HOST", "tcp://"+host+":"+port.Port())
	t.Setenv("CAUTEM_MAIN_PROCESS_RESTART_POLICY", "")
	t.Setenv("CAUTEM_PODMAN_CONFIG", `{"network_name":"cautem-cli-reconnect-`+time.Now().Format("150405000000000")+`","image_pull_policy":"missing","health_check_interval_secs":0,"userns":"host"}`)
	app := New()
	if app.Sandboxes == nil || app.Sandboxes.Driver == nil {
		t.Fatal("configured Podman driver is unavailable")
	}
	app.SetCommandContext(ctx)

	nonTTY := "podman-reconnect-" + time.Now().Format("150405000000000")
	createDetachedSandbox(t, app, SandboxCreateOpts{
		Name: nonTTY, Image: cliPodmanSandboxImage, Workspace: workspace, NoProxy: true, NoHarden: true, NoVolume: true,
		Detach: true, Argv: []string{"sh", "-c", "echo CAUTEM_PODMAN_RECONNECT_STARTED; sleep 5; echo CAUTEM_PODMAN_RECONNECT_FINISHED; exit 19"},
	})
	output, attachErr := captureStdout(t, func() error { return app.SandboxAttach(nonTTY, false) })
	if !strings.Contains(output, "CAUTEM_PODMAN_RECONNECT_STARTED") || !strings.Contains(output, "CAUTEM_PODMAN_RECONNECT_FINISHED") {
		t.Fatalf("Podman non-TTY reconnect output=%q; want output before and after attach", output)
	}
	if code, ok := ExitCode(attachErr); !ok || code != 19 {
		t.Fatalf("Podman non-TTY attach error=%v, exit=(%d,%v); want 19", attachErr, code, ok)
	}
	if err := app.Sandboxes.Remove(ctx, nonTTY); err != nil {
		t.Fatalf("remove non-TTY Podman sandbox: %v", err)
	}

	tty := "podman-tty-reconnect-" + time.Now().Format("150405000000000")
	createDetachedSandbox(t, app, SandboxCreateOpts{
		Name: tty, Image: cliPodmanSandboxImage, Workspace: workspace, NoProxy: true, NoHarden: true, NoVolume: true,
		ForceTTY: true, Detach: true, Argv: []string{"sh", "-c", "read value; printf 'CAUTEM_PODMAN_TTY_RECONNECT:%s\\n' \"$value\"; exit 7"},
	})
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
	output, attachErr = captureTTY(t, input, func() error { return app.SandboxAttach(tty, true) })
	_ = input.Close()
	if !strings.Contains(output, "CAUTEM_PODMAN_TTY_RECONNECT:reattached-input") {
		t.Fatalf("Podman TTY reconnect output=%q; want response to reattached stdin", output)
	}
	if code, ok := ExitCode(attachErr); !ok || code != 7 {
		t.Fatalf("Podman TTY attach error=%v, exit=(%d,%v); want 7", attachErr, code, ok)
	}
	if err := app.Sandboxes.Remove(ctx, tty); err != nil {
		t.Fatalf("remove TTY Podman sandbox: %v", err)
	}
}

func createDetachedSandbox(t *testing.T, app *App, options SandboxCreateOpts) {
	t.Helper()
	if _, err := captureStdout(t, func() error { return app.SandboxCreate(options) }); err != nil {
		t.Fatalf("create detached Podman sandbox %q: %v", options.Name, err)
	}
}

func captureStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()
	previous := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	callErr := run()
	_ = writer.Close()
	os.Stdout = previous
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatalf("capture process output: %v", readErr)
	}
	return string(output), callErr
}

func captureTTY(t *testing.T, input *os.File, run func() error) (string, error) {
	t.Helper()
	previousStdin := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = previousStdin }()
	return captureStdout(t, run)
}
