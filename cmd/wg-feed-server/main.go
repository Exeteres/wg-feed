package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/exeteres/wg-feed/internal/logx"
	"github.com/exeteres/wg-feed/internal/server/app"
	"github.com/exeteres/wg-feed/internal/server/config"
)

func main() {
	_ = godotenv.Load()

	logger := logx.NewStdoutLogger()

	cfg, err := config.FromEnv()
	if err != nil {
		logger.Error("config error", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	stdLogger := slog.NewLogLogger(logger.Handler(), slog.LevelInfo)
	if err := app.Run(ctx, cfg, stdLogger); err != nil {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}
