package service

import (
	"context"
	"log/slog"

	"github.com/cautem/quarshell-cli/internal/logger"
	"github.com/cautem/quarshell-slogx"
)

// opLogger returns a context logger tagged with a stable operation name
// (same convention as gateway/agent and team-tasks: package.resource.action).
func opLogger(ctx context.Context, op string, attrs ...any) *slogx.Logger {
	if ctx == nil {
		ctx = context.Background()
	}
	args := make([]any, 0, 1+len(attrs))
	args = append(args, slog.String("op", op))
	args = append(args, attrs...)
	return logger.FromContext(ctx).With(args...)
}

// op is the preferred logger helper for App methods (uses command context).
func (a *App) op(name string, attrs ...any) *slogx.Logger {
	return opLogger(a.CommandContext(), name, attrs...)
}

// cliOp is kept for call sites that do not have *App; prefer (a *App).op.
func cliOp(op string, attrs ...any) *slogx.Logger {
	return opLogger(context.Background(), op, attrs...)
}
