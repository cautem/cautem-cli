package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	core "github.com/cautem/cautem-core"
	"github.com/cautem/cautem-driver/driver"
	"golang.org/x/term"
)

func canonicalMainCommand(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	return append([]string(nil), argv...)
}

func canonicalMainTTY(argv []string, requested bool) bool {
	return len(argv) > 0 && requested
}

func attachCanonicalMainProcess(ctx context.Context, compute driver.ComputeDriver, id core.ID, output io.Writer) error {
	waiter, ok := compute.(driver.MainProcessWaiter)
	if !ok {
		return errors.New("sandbox create: compute driver cannot report the canonical main-process exit status")
	}
	attacher, ok := compute.(driver.MainProcessLogAttacher)
	if !ok {
		return errors.New("sandbox create: compute driver cannot attach to canonical main-process output")
	}
	logs, err := attacher.OpenMainProcessLogs(ctx, id)
	if err != nil {
		return err
	}
	logsDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(output, logs)
		logsDone <- copyErr
	}()
	code, waitErr := waiter.WaitMainProcess(ctx, id)
	if waitErr != nil {
		_ = logs.Close()
	}
	var logsErr error
	select {
	case logsErr = <-logsDone:
	case <-ctx.Done():
		_ = logs.Close()
		logsErr = <-logsDone
	case <-time.After(5 * time.Second):
		_ = logs.Close()
		logsErr = <-logsDone
	}
	if waitErr != nil {
		return waitErr
	}
	if logsErr != nil && !errors.Is(logsErr, context.Canceled) {
		if !errors.Is(logsErr, io.ErrClosedPipe) && !errors.Is(logsErr, io.EOF) {
			return logsErr
		}
	}
	if code != 0 {
		return &ExitError{Code: code}
	}
	return nil
}

// SandboxAttach reconnects the CLI to an already running canonical process.
// TTY mode must be selected explicitly because the container driver contract
// does not persist terminal metadata in Info.
func (a *App) SandboxAttach(name string, tty bool) error {
	if a == nil {
		return errors.New("sandbox attach: compute driver is not configured")
	}
	ctx, cancel := a.withTimeout(TimeoutWork)
	defer cancel()
	client, remote, err := a.lifecycleGatewaySandbox(ctx, name)
	if err != nil {
		return err
	}
	if remote {
		defer client.Close()
		return attachGatewayMainProcess(ctx, client, a.workspaceName(), name, tty)
	}
	if a.Sandboxes == nil || a.Sandboxes.Driver == nil {
		return errors.New("sandbox attach: compute driver is not configured")
	}
	info, err := a.Sandboxes.Driver.Inspect(ctx, name)
	if err != nil {
		return fmt.Errorf("sandbox attach inspect %q: %w", name, err)
	}
	if !strings.EqualFold(info.Status, "running") {
		return fmt.Errorf("sandbox attach: %q is %s; attach requires a running sandbox", info.Name, info.Status)
	}
	if tty {
		return attachCanonicalMainProcessTTY(ctx, a.Sandboxes.Driver, info.ID, os.Stdin, os.Stdout, int(os.Stdout.Fd()))
	}
	return attachCanonicalMainProcess(ctx, a.Sandboxes.Driver, info.ID, os.Stdout)
}

func attachCanonicalMainProcessTTY(ctx context.Context, compute driver.ComputeDriver, id core.ID, stdin io.Reader, stdout io.Writer, sizeFD int) error {
	ttyDriver, ok := compute.(driver.MainProcessTTY)
	if !ok {
		return errors.New("sandbox create: compute driver cannot attach to a canonical TTY process")
	}
	if stdin == nil {
		stdin = os.Stdin
	}
	cols, rows := uint32(0), uint32(0)
	if term.IsTerminal(sizeFD) {
		if width, height, err := term.GetSize(sizeFD); err == nil && width > 0 && height > 0 {
			cols, rows = uint32(width), uint32(height)
		}
	}
	session, err := ttyDriver.OpenMainProcessTTY(ctx, id, cols, rows)
	if err != nil {
		return err
	}
	defer session.Close()

	var restore func() error
	if term.IsTerminal(int(os.Stdin.Fd())) {
		state, rawErr := term.MakeRaw(int(os.Stdin.Fd()))
		if rawErr != nil {
			return fmt.Errorf("sandbox create enable terminal raw mode: %w", rawErr)
		}
		restore = func() error { return term.Restore(int(os.Stdin.Fd()), state) }
		defer restore()
	}
	if cols > 0 && rows > 0 {
		stopResize := watchTerminalResize(ctx, mainProcessResizer{resize: func(cols, rows uint32) error {
			return ttyDriver.ResizeMainProcessTTY(ctx, id, cols, rows)
		}})
		defer stopResize()
	}

	outputDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(stdout, session)
		outputDone <- copyErr
	}()
	inputDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(session, stdin)
		inputDone <- copyErr
	}()
	waitDone := make(chan mainProcessWaitResult, 1)
	go func() {
		waiter, ok := compute.(driver.MainProcessWaiter)
		if !ok {
			waitDone <- mainProcessWaitResult{err: errors.New("sandbox create: compute driver cannot report the canonical main-process exit status")}
			return
		}
		code, waitErr := waiter.WaitMainProcess(ctx, id)
		waitDone <- mainProcessWaitResult{code: code, err: waitErr}
	}()

	var waited mainProcessWaitResult
	select {
	case waited = <-waitDone:
	case <-ctx.Done():
		_ = session.Close()
		return ctx.Err()
	}
	var outputErr error
	select {
	case outputErr = <-outputDone:
	case <-ctx.Done():
		_ = session.Close()
		outputErr = <-outputDone
	case <-time.After(5 * time.Second):
		_ = session.Close()
		outputErr = <-outputDone
	}
	if waited.err != nil {
		return waited.err
	}
	if outputErr != nil && !errors.Is(outputErr, io.EOF) && !errors.Is(outputErr, net.ErrClosed) {
		return fmt.Errorf("sandbox create read canonical TTY output: %w", outputErr)
	}
	var inputErr error
	select {
	case inputErr = <-inputDone:
	default:
		// A terminal input read cannot be interrupted portably after remote exit.
	}
	if inputErr != nil && !errors.Is(inputErr, io.ErrClosedPipe) && !errors.Is(inputErr, net.ErrClosed) && !errors.Is(inputErr, io.EOF) {
		return fmt.Errorf("sandbox create write canonical TTY input: %w", inputErr)
	}
	if waited.code != 0 {
		return &ExitError{Code: waited.code}
	}
	return nil
}

type mainProcessWaitResult struct {
	code int
	err  error
}

type mainProcessResizer struct {
	resize func(uint32, uint32) error
}

func (r mainProcessResizer) Resize(cols, rows uint32) error {
	return r.resize(cols, rows)
}
