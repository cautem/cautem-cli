//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package service

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

func watchTerminalResize(ctx context.Context, session resizableExecSession) func() {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return func() {}
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	stop := watchResizeEvents(ctx, signals, func() (uint32, uint32, error) {
		width, height, err := term.GetSize(int(os.Stdout.Fd()))
		return uint32(width), uint32(height), err
	}, session)
	return func() {
		signal.Stop(signals)
		stop()
	}
}
