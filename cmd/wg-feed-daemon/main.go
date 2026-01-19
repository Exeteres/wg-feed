package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/daemon"
)

func main() {
	_ = godotenv.Load()

	logger := log.New(os.Stdout, "wg-feed-daemon ", log.LstdFlags|log.LUTC)

	cfg, err := config.FromEnv()
	if err != nil {
		logger.Fatalf("config error: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := daemon.Run(ctx, cfg, logger); err != nil {
		logger.Fatalf("run error: %v", err)
	}
}
