package backend

import (
	"context"
	"fmt"
	"log"

	"github.com/exeteres/wg-feed/internal/client/backend/networkmanager"
	"github.com/exeteres/wg-feed/internal/client/backend/wgquick"
	"github.com/exeteres/wg-feed/internal/client/backend/windows"
	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/execx"
)

type Backend interface {
	Apply(ctx context.Context, name string, wgQuickConfig string, enabled bool) error
	Remove(ctx context.Context, name string) error
}

func New(cfg config.Config, logger *log.Logger) (Backend, error) {
	runner := execx.Runner{}
	switch cfg.Backend {
	case config.BackendWGQuick:
		return wgquick.New(runner, logger), nil
	case config.BackendNetworkManager:
		return networkmanager.New(runner, logger), nil
	case config.BackendWindows:
		return windows.New(runner, logger), nil
	default:
		return nil, fmt.Errorf("unknown backend %q", cfg.Backend)
	}
}
