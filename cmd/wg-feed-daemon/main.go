package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/daemon"
	"github.com/exeteres/wg-feed/internal/logx"
)

func main() {
	configPath := flag.String("config", "", "path to wg-feed config file (default: /etc/wg-feed/config.{yaml,yml,toml,json})")
	flag.Parse()

	logger := logx.NewStdoutLogger()

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("config error", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := daemon.Run(ctx, cfg, logger); err != nil {
		logger.Error("run error", "err", err)
		os.Exit(1)
	}
}
