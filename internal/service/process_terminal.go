package service

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/cautem/quarshell-driver/driver"
	"golang.org/x/term"
)

func localExecTerminalExtension(compute driver.ComputeDriver) driver.ExecTerminal {
	terminal, ok := compute.(driver.ExecTerminal)
	if !ok {
		return nil
	}
	provider, ok := compute.(driver.BackendCapabilityProvider)
	if !ok || !provider.BackendCapabilityInfo().Has(driver.CapabilityExecTerminal) {
		return nil
	}
	return terminal
}

func runLocalExecTerminal(ctx context.Context, compute driver.ComputeDriver, terminal driver.ExecTerminal, opt ExecOpts, env []string) (int, error) {
	info, err := compute.Inspect(ctx, opt.Name)
	if err != nil {
		return -1, err
	}
	columns, rows := uint32(80), uint32(24)
	if width, height, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 && height > 0 {
		columns, rows = uint32(width), uint32(height)
	}
	session, err := terminal.OpenExecTerminal(ctx, info.ID, driver.ExecTerminalRequest{
		Argv: append([]string(nil), opt.Argv...), Env: append([]string(nil), env...), WorkDir: opt.WorkDir,
		NoLoginShell: opt.NoLoginShell, Columns: columns, Rows: rows,
	})
	if err != nil {
		return -1, err
	}
	defer session.Close()

	input := opt.Stdin
	if input == nil {
		input = os.Stdin
	}
	var restore func() error
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		state, err := term.MakeRaw(int(file.Fd()))
		if err != nil {
			return -1, err
		}
		restore = func() error { return term.Restore(int(file.Fd()), state) }
		defer restore()
	}
	stopResize := watchTerminalResize(ctx, localExecTerminalResizer{ctx: ctx, session: session})
	defer stopResize()

	inputDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(session, input)
		if copyErr == nil {
			_, copyErr = session.Write([]byte{4})
		}
		inputDone <- copyErr
	}()
	_, outputErr := io.Copy(os.Stdout, session)
	if ctx.Err() != nil {
		return -1, errors.Join(ctx.Err(), outputErr)
	}
	exitCode, waitErr := session.Wait(ctx)
	if ctx.Err() != nil {
		return -1, errors.Join(ctx.Err(), outputErr, waitErr)
	}
	var inputErr error
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		select {
		case inputErr = <-inputDone:
		default:
		}
	} else {
		inputErr = <-inputDone
	}
	return exitCode, errors.Join(outputErr, waitErr, inputErr)
}

type localExecTerminalResizer struct {
	ctx     context.Context
	session driver.ExecTerminalSession
}

func (resizer localExecTerminalResizer) Resize(columns, rows uint32) error {
	return resizer.session.Resize(resizer.ctx, columns, rows)
}
