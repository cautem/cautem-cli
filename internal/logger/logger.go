// Package logger wraps slogx for the cautem CLI process.
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/cautem/quarshell-slogx"
)

const (
	EnvLogLevel     = "CAUTEM_LOG_LEVEL"
	EnvLogFormat    = "CAUTEM_LOG_FORMAT"
	EnvLogLevelAddr = "CAUTEM_LOG_LEVEL_ADDR"
)

// Options tweak Setup.
type Options struct {
	Service string
	Output  io.Writer
	Level   slog.Level
	Format  string
	Dev     bool
}

// Setup builds a corporate-masked slogx logger and installs it as slog.Default().
func Setup(ctx context.Context, opt Options) *slogx.Logger {
	if opt.Output == nil {
		opt.Output = os.Stderr
	}

	var format slogx.Format
	if opt.Dev {
		format = slogx.FormatText
	} else if opt.Format != "" {
		format = slogx.ParseFormat(opt.Format)
	} else {
		format = slogx.ParseFormat(envOr(EnvLogFormat, "json"))
	}

	level := opt.Level
	if raw := os.Getenv(EnvLogLevel); raw != "" {
		if lvl, err := slogx.ParseLevel(raw); err == nil {
			level = lvl
		}
	} else if opt.Dev && level == 0 {
		level = slog.LevelDebug
	} else if level == 0 {
		level = slog.LevelInfo
	}

	log := slogx.SetupDefault(
		slogx.WithOutput(opt.Output),
		slogx.WithFormat(format),
		slogx.WithLevel(level),
		slogx.WithCorporateMasking(),
		slogx.WithAddSource(!opt.Dev),
		slogx.WithStackOnError(true),
		slogx.WithTraceContext(true),
		slogx.WithContextKeys(slogx.KeyRequestID, slogx.KeyUserID, slogx.KeyTenantID),
	)
	if opt.Service != "" {
		log = log.With("service", opt.Service)
		slog.SetDefault(log.Logger)
	}

	go log.WatchLevelEnv(ctx, EnvLogLevel, 0)
	if addr := strings.TrimSpace(os.Getenv(EnvLogLevelAddr)); addr != "" {
		go func() {
			_, _, _ = log.ListenLevelHTTP(ctx, addr)
		}()
	}
	return log
}

// FromContext returns the logger from ctx or slogx default.
func FromContext(ctx context.Context) *slogx.Logger {
	return slogx.FromContext(ctx)
}

// ToContext stores log in ctx.
func ToContext(ctx context.Context, log *slogx.Logger) context.Context {
	return slogx.ToContext(ctx, log)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
