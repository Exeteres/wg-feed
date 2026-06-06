//go:build !windows

package main

import (
	"context"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/exeteres/wg-feed/internal/client/config"
)

func runDaemonEntry(cfg config.Config, logger *slog.Logger, run func(context.Context, config.Config, *slog.Logger) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return run(ctx, cfg, logger)
}
