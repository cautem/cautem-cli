package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cautem/quarshell-sdk/go/cautem"
	"golang.org/x/term"
)

func attachGatewayMainProcess(ctx context.Context, client *cautem.Client, workspace, name string, tty bool) error {
	cols, rows := uint32(0), uint32(0)
	if tty && term.IsTerminal(int(os.Stdout.Fd())) {
		if width, height, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 && height > 0 {
			cols, rows = uint32(width), uint32(height)
		}
	}
	session, err := client.AttachControlMainProcess(ctx, workspace, name, tty, cols, rows)
	if err != nil {
		return fmt.Errorf("sandbox attach %q: %w", name, err)
	}
	defer session.Close()
	if tty {
		return attachGatewayMainProcessTTY(ctx, session, cols, rows)
	}
	if _, err := io.Copy(os.Stdout, session); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("sandbox attach read output: %w", err)
	}
	code, err := session.ExitCode()
	if err != nil {
		return fmt.Errorf("sandbox attach wait for process: %w", err)
	}
	if code != 0 {
		return &ExitError{Code: code}
	}
	return nil
}

func attachGatewayMainProcessTTY(ctx context.Context, session cautem.SandboxMainProcessSession, cols, rows uint32) error {
	var restore func() error
	if term.IsTerminal(int(os.Stdin.Fd())) {
		state, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("sandbox attach enable terminal raw mode: %w", err)
		}
		restore = func() error { return term.Restore(int(os.Stdin.Fd()), state) }
		defer restore()
	}
	if cols > 0 && rows > 0 {
		stopResize := watchTerminalResize(ctx, mainProcessResizer{resize: session.Resize})
		defer stopResize()
	}
	inputDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(session, os.Stdin)
		inputDone <- err
	}()
	_, outputErr := io.Copy(os.Stdout, session)
	if outputErr != nil && !errors.Is(outputErr, io.EOF) {
		return fmt.Errorf("sandbox attach read TTY output: %w", outputErr)
	}
	code, err := session.ExitCode()
	if err != nil {
		return fmt.Errorf("sandbox attach wait for process: %w", err)
	}
	select {
	case <-inputDone:
	case <-time.After(100 * time.Millisecond):
	}
	if code != 0 {
		return &ExitError{Code: code}
	}
	return nil
}
