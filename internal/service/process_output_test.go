package service

import (
	"io"
	"os"
	"testing"

	"github.com/cautem/quarshell-driver/driver"
)

func TestWriteExecResultWritesCapturedStreams(t *testing.T) {
	stdout, stderr := os.Stdout, os.Stderr
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outWrite, errWrite
	t.Cleanup(func() {
		os.Stdout, os.Stderr = stdout, stderr
		_ = outWrite.Close()
		_ = errWrite.Close()
		_ = outRead.Close()
		_ = errRead.Close()
	})
	if err := writeExecResult(driver.ExecResult{Stdout: []byte("out"), Stderr: []byte("err"), OutputTruncated: true}); err != nil {
		t.Fatal(err)
	}
	_ = outWrite.Close()
	_ = errWrite.Close()
	out, err := io.ReadAll(outRead)
	if err != nil {
		t.Fatal(err)
	}
	warning, err := io.ReadAll(errRead)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "out" {
		t.Fatalf("stdout = %q", out)
	}
	if string(warning) != "errcautem: command output was truncated by the compute driver\n" {
		t.Fatalf("stderr = %q", warning)
	}
}
