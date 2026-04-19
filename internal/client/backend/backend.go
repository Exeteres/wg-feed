package backend

import (
	"fmt"
	"log/slog"

	"github.com/exeteres/wg-feed/internal/client/backend/netns"
	"github.com/exeteres/wg-feed/internal/client/backend/networkmanager"
	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/backend/wgquick"
	"github.com/exeteres/wg-feed/internal/client/backend/windows"
	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/execx"
)

type Backend = shared.Backend

func NewForType(backendType config.BackendType, logger *slog.Logger) (Backend, error) {
	runner := execx.ShellRunner{}
	backendLogger := logger.With("backend", string(backendType))
	switch backendType {
	case config.BackendWGQuick:
		return wgquick.New(runner, backendLogger), nil
	case config.BackendNetworkManager:
		return networkmanager.New(runner, backendLogger), nil
	case config.BackendNetNS:
		return netns.New(runner, backendLogger), nil
	case config.BackendWindows:
		return windows.New(runner, backendLogger), nil
	default:
		return nil, fmt.Errorf("unknown backend %q", backendType)
	}
}
