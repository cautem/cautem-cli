//go:build !darwin && !linux

package service

import (
	"context"
	"fmt"

	"github.com/cautem/quarshell-proxy/proxy"
)

func serveUnixProxy(_ context.Context, _ *proxy.Server, _ string) error {
	return fmt.Errorf("proxy Unix sockets are unsupported on this platform")
}
