package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/exeteres/wg-feed/internal/etcd"
	"github.com/exeteres/wg-feed/internal/upload"
)

func main() {
	_ = godotenv.Load()
	logger := log.New(os.Stdout, "wg-feed-upload ", log.LstdFlags|log.LUTC)

	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ttlSeconds := fs.Int("ttl", 15*60, "ttl_seconds for the success response")
	if err := fs.Parse(os.Args[1:]); err != nil {
		logger.Fatalf("usage: %s [--ttl 900] <feedPath>", os.Args[0])
	}
	args := fs.Args()

	if len(args) != 1 {
		logger.Fatalf("usage: %s [--ttl 900] <feedPath>", os.Args[0])
	}

	feedPath, err := upload.ParseFeedPath(args[0])
	if err != nil {
		logger.Fatalf("feedPath error: %v", err)
	}
	if err := upload.ValidateTTLSeconds(*ttlSeconds); err != nil {
		logger.Fatalf("ttl error: %v", err)
	}

	endpoints, err := etcd.EndpointsFromEnv()
	if err != nil {
		logger.Fatalf("config error: %v", err)
	}

	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		logger.Fatalf("read stdin: %v", err)
	}

	parsed, err := upload.ParseInput(string(body))
	if err != nil {
		logger.Fatalf("input error: %v", err)
	}
	storeBody, revision, err := upload.BuildStoreBodyJSON(*ttlSeconds, parsed)
	if err != nil {
		logger.Fatalf("encode feed entry: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cli, err := etcd.NewClient(endpoints)
	if err != nil {
		logger.Fatalf("create etcd client: %v", err)
	}
	defer cli.Close()

	st := etcd.NewStore(cli)
	key := "wg-feed/feeds/" + feedPath
	if err := st.Put(ctx, key, storeBody); err != nil {
		logger.Fatalf("put key %q: %v", key, err)
	}

	_, _ = fmt.Fprintf(os.Stdout, "Uploaded feed to %s (revision=%s)\n", key, revision)
}
