package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	client "github.com/exeteres/wg-feed/internal/client"
	"github.com/exeteres/wg-feed/internal/client/config"
)

func main() {
	_ = godotenv.Load()

	logger := log.New(os.Stdout, "wg-feed-apply ", log.LstdFlags|log.LUTC)

	cfg, err := config.FromEnv()
	if err != nil {
		logger.Fatalf("config error: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := client.RunOnce(ctx, cfg, cfg.SetupURLs, logger); err != nil {
		logger.Fatalf("run error: %v", err)
	}
}
