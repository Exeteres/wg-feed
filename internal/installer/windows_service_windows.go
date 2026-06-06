//go:build windows

package installer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const windowsEventSourceName = "WG Feed Daemon"

func ensureWindowsService(_ context.Context, serviceName string, daemonPath string, configPath string) error {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return fmt.Errorf("ServiceName is required")
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	s, err := m.OpenService(serviceName)
	if err != nil {
		if !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return fmt.Errorf("open service %q: %w", serviceName, err)
		}
		cfg := mgr.Config{DisplayName: "WG Feed Daemon", StartType: mgr.StartAutomatic}
		s, err = m.CreateService(serviceName, daemonPath, cfg, "--config", configPath)
		if err != nil {
			return fmt.Errorf("create service %q: %w", serviceName, err)
		}
		defer func() { _ = s.Close() }()

		if err := ensureWindowsEventSource(windowsEventSourceName); err != nil {
			_ = s.Delete()
			return fmt.Errorf("install event log source %q: %w", serviceName, err)
		}
		return nil
	}
	defer func() { _ = s.Close() }()

	cfg, err := s.Config()
	if err != nil {
		return fmt.Errorf("read service %q config: %w", serviceName, err)
	}
	cfg.StartType = mgr.StartAutomatic
	cfg.DisplayName = "WG Feed Daemon"
	cfg.BinaryPathName = fmt.Sprintf("\"%s\" --config \"%s\"", daemonPath, configPath)
	if err := s.UpdateConfig(cfg); err != nil {
		return fmt.Errorf("update service %q config: %w", serviceName, err)
	}
	if err := ensureWindowsEventSource(windowsEventSourceName); err != nil {
		return fmt.Errorf("ensure event log source %q: %w", serviceName, err)
	}
	return nil
}

func ensureWindowsEventSource(serviceName string) error {
	err := eventlog.InstallAsEventCreate(serviceName, eventlog.Info|eventlog.Warning|eventlog.Error)
	if err == nil {
		return nil
	}
	if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "exists") {
		return nil
	}
	return err
}

func startWindowsService(_ context.Context, serviceName string) error {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return fmt.Errorf("ServiceName is required")
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("open service %q: %w", serviceName, err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service %q: %w", serviceName, err)
	}
	return nil
}

func stopWindowsService(_ context.Context, serviceName string) error {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return fmt.Errorf("ServiceName is required")
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	s, err := m.OpenService(serviceName)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		return fmt.Errorf("open service %q: %w", serviceName, err)
	}
	defer func() { _ = s.Close() }()

	_, err = s.Control(svc.Stop)
	if err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("stop service %q: %w", serviceName, err)
	}
	return nil
}

func deleteWindowsService(_ context.Context, serviceName string) error {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return fmt.Errorf("ServiceName is required")
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	s, err := m.OpenService(serviceName)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		return fmt.Errorf("open service %q: %w", serviceName, err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service %q: %w", serviceName, err)
	}
	if err := eventlog.Remove(windowsEventSourceName); err != nil && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return fmt.Errorf("remove event log source %q: %w", windowsEventSourceName, err)
	}
	return nil
}

func windowsServiceExists(_ context.Context, serviceName string) (bool, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return false, fmt.Errorf("ServiceName is required")
	}

	m, err := mgr.Connect()
	if err != nil {
		return false, fmt.Errorf("connect service manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	s, err := m.OpenService(serviceName)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return false, nil
		}
		return false, fmt.Errorf("open service %q: %w", serviceName, err)
	}
	_ = s.Close()
	return true, nil
}

func isWindowsServiceAlreadyRunningError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING)
}
