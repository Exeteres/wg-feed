package wgquick

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/execx"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) (execx.Result, error)
}

type Backend struct {
	runner Runner
	logger *log.Logger
}

func New(runner Runner, logger *log.Logger) *Backend {
	return &Backend{runner: runner, logger: logger}
}

func (b *Backend) Apply(ctx context.Context, name string, wgQuickConfig string, enabled bool) error {
	iface := strings.TrimSpace(name)
	if iface == "" {
		return errors.New("wg-quick backend requires a non-empty tunnel name")
	}
	if !strings.HasSuffix(wgQuickConfig, "\n") {
		wgQuickConfig += "\n"
	}

	tmpDir, err := os.MkdirTemp("", "wg-feed-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	configPath := filepath.Join(tmpDir, iface+".conf")
	if err := os.WriteFile(configPath, []byte(wgQuickConfig), 0o600); err != nil {
		return err
	}

	if enabled {
		if isUp(ctx, b.runner, iface) {
			if ok := bestEffortDeviceUpdate(ctx, b, configPath, iface); ok {
				return nil
			}
		}
		// Fall back to wg-quick (down/up) when interface isn't up or device update fails.
		_, _ = b.runner.Run(ctx, "wg-quick", "down", iface)
		_, err := b.runner.Run(ctx, "wg-quick", "up", configPath)
		return err
	}
	_, err = b.runner.Run(ctx, "wg-quick", "down", iface)
	return err
}

func (b *Backend) Remove(ctx context.Context, name string) error {
	iface := strings.TrimSpace(name)
	if iface == "" {
		return nil
	}
	_, _ = b.runner.Run(ctx, "wg-quick", "down", iface)
	return nil
}

func isUp(ctx context.Context, runner Runner, iface string) bool {
	_, err := runner.Run(ctx, "wg", "show", iface)
	return err == nil
}

func bestEffortDeviceUpdate(ctx context.Context, b *Backend, configPath string, iface string) bool {
	stripRes, err := b.runner.Run(ctx, "wg-quick", "strip", configPath)
	if err != nil {
		b.logf("wg-quick strip failed iface=%q err=%v", iface, err)
		return false
	}
	stripped := strings.TrimSpace(stripRes.Stdout)
	if stripped == "" {
		b.logf("wg-quick strip returned empty config iface=%q", iface)
		return false
	}

	f, err := os.CreateTemp("", "wg-feed-*.conf")
	if err != nil {
		return false
	}
	tmp := f.Name()
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmp)
	}()

	var buf bytes.Buffer
	buf.WriteString(stripped)
	buf.WriteByte('\n')
	if _, err := f.Write(buf.Bytes()); err != nil {
		return false
	}
	if err := f.Close(); err != nil {
		return false
	}

	// Prefer syncconf (removes peers not in config); fall back to setconf.
	if _, err := b.runner.Run(ctx, "wg", "syncconf", iface, tmp); err == nil {
		return true
	} else {
		b.logf("wg syncconf failed iface=%q err=%v", iface, err)
	}
	if _, err := b.runner.Run(ctx, "wg", "setconf", iface, tmp); err == nil {
		return true
	} else {
		b.logf("wg setconf failed iface=%q err=%v", iface, err)
	}
	return false
}

func (b *Backend) logf(format string, args ...any) {
	if b.logger == nil {
		return
	}
	b.logger.Printf(format, args...)
}
