package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/exeteres/wg-feed/internal/etcd"
	"github.com/exeteres/wg-feed/internal/server/config"
	"github.com/exeteres/wg-feed/internal/server/httpapi"
)

func Run(ctx context.Context, cfg config.Config, logger *log.Logger) error {
	etcdClient, err := etcd.NewClient(cfg.EtcdEndpoints)
	if err != nil {
		return fmt.Errorf("create etcd client: %w", err)
	}
	defer func() {
		if cerr := etcdClient.Close(); cerr != nil {
			logger.Printf("close etcd client: %v", cerr)
		}
	}()

	st := etcd.NewStore(etcdClient)
	h := httpapi.NewHandler(st, logger)

	addr := ":" + cfg.ServerPort
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	defer func() {
		if cerr := ln.Close(); cerr != nil {
			logger.Printf("close listener: %v", cerr)
		}
	}()

	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	logger.Printf("listening on %s", ln.Addr().String())

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err == nil || err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
