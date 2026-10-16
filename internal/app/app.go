// Package app is the composition root for the cautem CLI binary.
package app

import (
	"context"

	"github.com/cautem/quarshell-cli/internal/app/cli"
	"github.com/cautem/quarshell-cli/internal/logger"
)

// Run initializes logging and executes the CLI.
func Run(ctx context.Context, args []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	log := logger.Setup(ctx, logger.Options{Service: "cautem"})
	ctx = logger.ToContext(ctx, log)
	return cli.Execute(ctx, args)
}
