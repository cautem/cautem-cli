//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package service

import "context"

func watchTerminalResize(_ context.Context, _ resizableExecSession) func() {
	return func() {}
}
