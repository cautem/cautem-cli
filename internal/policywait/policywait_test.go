package policywait_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cautem/quarshell-cli/internal/policywait"
)

func TestFileAppliedSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	want := []byte("version: 1\n")
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Unix(100, 0)
	now := start
	err := policywait.FileApplied(context.Background(), path, want, policywait.Options{
		Settle:  50 * time.Millisecond,
		Timeout: time.Second,
		Poll:    10 * time.Millisecond,
		Now:     func() time.Time { return now },
		Sleep: func(_ context.Context, d time.Duration) error {
			now = now.Add(d)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFileAppliedTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.yaml")
	start := time.Unix(0, 0)
	now := start
	err := policywait.FileApplied(context.Background(), path, []byte("x"), policywait.Options{
		Settle:  time.Millisecond,
		Timeout: 30 * time.Millisecond,
		Poll:    10 * time.Millisecond,
		Now:     func() time.Time { return now },
		Sleep: func(_ context.Context, d time.Duration) error {
			now = now.Add(d)
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected timeout")
	}
	if policywait.ExitCode(err) != 124 {
		t.Fatalf("exit=%d err=%v", policywait.ExitCode(err), err)
	}
}
