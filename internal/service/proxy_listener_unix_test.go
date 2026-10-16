//go:build darwin || linux

package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cautem/cautem-core/engine"
	"github.com/cautem/cautem-proxy/proxy"
)

func TestServeUnixProxyUsesDefaultDenyAndCleansSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "cpx-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "proxy.sock")
	ctx, cancel := context.WithCancel(context.Background())
	server := proxy.NewServer(&engine.Allowlist{}, io.Discard)
	serveDone := make(chan error, 1)
	serveStopped := false
	go func() {
		serveDone <- serveUnixProxy(ctx, server, path)
	}()
	t.Cleanup(func() {
		cancel()
		if serveStopped {
			return
		}
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Error("proxy did not stop after context cancellation")
		}
	})

	var response *http.Response
	var requestErr error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response, requestErr = unixProxyRequest(path)
		if response != nil {
			break
		}
		select {
		case err := <-serveDone:
			serveDone <- err
			t.Fatalf("proxy stopped before accepting a request: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if response == nil {
		t.Fatalf("proxy did not accept a request on the Unix socket: %v", requestErr)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("default-deny request status = %d, body = %q; want 403", response.StatusCode, body)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || !strings.Contains(string(body), "cautem-proxy: denied") {
		t.Fatalf("default-deny response body = %q, err = %v", body, err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("proxy socket was not present while serving: %v", err)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("proxy socket mode = %04o, want 0600", got)
	}

	cancel()
	select {
	case err := <-serveDone:
		serveStopped = true
		if err != context.Canceled {
			t.Fatalf("serveUnixProxy() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not stop after context cancellation")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("proxy socket remains after shutdown: %v", err)
	}
}

func TestListenUnixProxyRejectsInsecureParentAndNonSocket(t *testing.T) {
	root := t.TempDir()
	openDir := filepath.Join(root, "open")
	if err := os.Mkdir(openDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := listenUnixProxy(filepath.Join(openDir, "proxy.sock")); err == nil {
		t.Fatal("listenUnixProxy() accepted a non-private parent directory")
	}

	privateDir := filepath.Join(root, "private")
	if err := os.Mkdir(privateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(privateDir, "proxy.sock")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := listenUnixProxy(path); err == nil {
		t.Fatal("listenUnixProxy() accepted an existing non-socket path")
	}
	if body, err := os.ReadFile(path); err != nil || string(body) != "keep" {
		t.Fatalf("non-socket path was changed: body=%q err=%v", body, err)
	}
}

func TestListenUnixProxyProtectsActiveStaleAndReplacedSockets(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "cpx-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "proxy.sock")
	active, err := listenUnixProxy(path)
	if err != nil {
		t.Fatal(err)
	}
	activeInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := listenUnixProxy(path); err == nil {
		t.Fatal("listenUnixProxy() removed an active socket")
	}
	if current, err := os.Lstat(path); err != nil || !os.SameFile(activeInfo, current) {
		t.Fatalf("active socket changed after refused bind: current=%v err=%v", current, err)
	}
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}

	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := listenUnixProxy(path)
	if err != nil {
		t.Fatalf("listenUnixProxy() did not replace stale socket: %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	replacement, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	replacement.SetUnlinkOnClose(false)
	replacementInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Close(); err != nil {
		t.Fatal(err)
	}
	if current, err := os.Lstat(path); err != nil || !os.SameFile(replacementInfo, current) {
		t.Fatalf("listener cleanup removed a replacement socket: current=%v err=%v", current, err)
	}
}

func unixProxyRequest(path string) (*http.Response, error) {
	connection, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := fmt.Fprint(connection, "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n"); err != nil {
		return nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		return nil, err
	}
	return response, nil
}
