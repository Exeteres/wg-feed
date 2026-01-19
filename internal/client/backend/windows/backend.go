package windows

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/execx"
)

type Backend struct {
	Runner execx.Runner
	Logger *log.Logger
}

func New(runner execx.Runner, logger *log.Logger) *Backend {
	return &Backend{Runner: runner, Logger: logger}
}

func (b *Backend) Apply(ctx context.Context, name string, wgQuickConfig string, enabled bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("windows backend requires a non-empty tunnel name")
	}
	// WireGuard for Windows uses wireguard.exe /installtunnelservice <configPath>
	// and /uninstalltunnelservice <tunnelName>.
	_, _ = b.Runner.Run(ctx, "wireguard.exe", "/uninstalltunnelservice", name)
	if !enabled {
		return nil
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
	configPath := filepath.Join(tmpDir, name+".conf")
	if err := os.WriteFile(configPath, []byte(wgQuickConfig), 0o600); err != nil {
		return err
	}
	_, err = b.Runner.Run(ctx, "wireguard.exe", "/installtunnelservice", configPath)
	return err
}

func (b *Backend) Remove(ctx context.Context, name string) error {
	_, _ = b.Runner.Run(ctx, "wireguard.exe", "/uninstalltunnelservice", name)
	return nil
}
