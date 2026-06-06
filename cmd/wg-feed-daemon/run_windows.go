//go:build windows

package main

import (
	"context"
	"errors"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/logx"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
)

const (
	windowsServiceName     = "wg-feed-daemon"
	windowsEventSourceName = "WG Feed Daemon"
)

func runDaemonEntry(cfg config.Config, logger *slog.Logger, run func(context.Context, config.Config, *slog.Logger) error) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return run(ctx, cfg, logger)
	}

	serviceLogger := logger
	elog, err := eventlog.Open(windowsEventSourceName)
	if err != nil {
		logger.Warn("open windows event log failed, falling back to default logger", "err", err)
	} else {
		defer func() { _ = elog.Close() }()
		serviceLogger = slog.New(newWindowsEventLogHandler(elog, logx.SlogLevelFromEnv()))
	}

	h := &daemonWindowsService{cfg: cfg, logger: serviceLogger, run: run}
	if err := svc.Run(windowsServiceName, h); err != nil {
		return err
	}
	return nil
}

type daemonWindowsService struct {
	cfg    config.Config
	logger *slog.Logger
	run    func(context.Context, config.Config, *slog.Logger) error
}

func (d *daemonWindowsService) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.run(ctx, d.cfg, d.logger)
	}()

	status <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				d.logger.Error("service run error", "err", err)
				return false, 1
			}
			status <- svc.Status{State: svc.Stopped}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
					d.logger.Error("service stop error", "err", err)
					status <- svc.Status{State: svc.Stopped}
					return false, 1
				}
				status <- svc.Status{State: svc.Stopped}
				return false, 0
			default:
			}
		}
	}
}
