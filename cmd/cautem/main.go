package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cautem/quarshell-cli/internal/app"
	"github.com/cautem/quarshell-cli/internal/logger"
	"github.com/cautem/quarshell-cli/internal/service"
	"github.com/cautem/quarshell-slogx"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args[1:]); err != nil {
		log := logger.FromContext(ctx)
		if ee, ok := errors.AsType[*service.ExitError](err); ok {
			log.Error("command failed", "exit_code", ee.Code, slogx.Err(err))
			os.Exit(ee.Code)
		}
		if errors.Is(err, context.Canceled) {
			log.Info("command canceled")
			os.Exit(130)
		}
		log.Error("command failed", slogx.Err(err))
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
