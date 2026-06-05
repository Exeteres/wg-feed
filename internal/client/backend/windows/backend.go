package windows

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/execx"
	"github.com/exeteres/wg-feed/internal/client/namegen"
	"github.com/exeteres/wg-feed/internal/client/state"
)

type Backend struct {
	Runner execx.Runner
	Logger *slog.Logger
}

type tunnelData struct {
	ServiceName string `json:"service_name"`
}

func resolveProgramDataDir() string {
	base := strings.TrimSpace(os.Getenv("ProgramData"))
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "wg-feed")
}

func tunnelConfigPath(serviceName string) string {
	return filepath.Join(resolveProgramDataDir(), "tunnels", serviceName+".conf")
}

func New(runner execx.Runner, logger *slog.Logger) *Backend {
	return &Backend{Runner: runner, Logger: logger}
}

func (b *Backend) Apply(ctx context.Context, tunnel shared.ResolvedTunnel, state state.TunnelState) (state.TunnelState, error) {
	b.Logger.Debug("windows apply started", "tunnel_id", tunnel.ID, "tunnel_name", tunnel.Name, "enabled", tunnel.EffectiveEnabled)
	enabled := tunnel.EffectiveEnabled
	tunnelData := tunnelData{}
	if len(state.Data) > 0 {
		_ = json.Unmarshal(state.Data, &tunnelData)
	}
	name := strings.TrimSpace(tunnelData.ServiceName)
	if name == "" {
		effective, err := namegen.EffectiveName(strings.TrimSpace(tunnel.Name), func(candidate string) (bool, error) {
			return b.serviceNameOccupied(ctx, candidate)
		})
		if err != nil {
			return state, err
		}
		name = effective
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return state, errors.New("windows backend requires a non-empty tunnel name")
	}
	tunnelData.ServiceName = name
	dataBytes, err := json.Marshal(tunnelData)
	if err != nil {
		return state, err
	}
	state.Data = dataBytes
	// WireGuard for Windows uses wireguard.exe /installtunnelservice <configPath>
	// and /uninstalltunnelservice <tunnelName>.
	b.Logger.Debug("windows uninstall tunnel service", "service_name", name)
	_, _ = b.Runner.Run(ctx, "wireguard.exe", "/uninstalltunnelservice", name)
	_ = os.Remove(tunnelConfigPath(name))
	if !enabled {
		b.Logger.Debug("windows apply disabled completed", "service_name", name)
		return state, nil
	}
	wgQuickConfig := tunnel.WGQuickConfig
	if !strings.HasSuffix(wgQuickConfig, "\n") {
		wgQuickConfig += "\n"
	}
	configPath := tunnelConfigPath(name)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return state, err
	}
	if err := os.WriteFile(configPath, []byte(wgQuickConfig), 0o600); err != nil {
		return state, err
	}
	b.Logger.Debug("windows install tunnel service", "service_name", name, "config_path", configPath)
	_, err = b.Runner.Run(ctx, "wireguard.exe", "/installtunnelservice", configPath)
	if err == nil {
		b.Logger.Debug("windows apply completed", "service_name", name)
	}
	return state, err
}

func (b *Backend) Remove(ctx context.Context, state state.TunnelState) error {
	tunnelData := tunnelData{}
	_ = json.Unmarshal(state.Data, &tunnelData)
	name := strings.TrimSpace(tunnelData.ServiceName)
	if name == "" {
		return nil
	}
	b.Logger.Debug("windows remove uninstall tunnel service", "service_name", name)
	_, _ = b.Runner.Run(ctx, "wireguard.exe", "/uninstalltunnelservice", name)
	_ = os.Remove(tunnelConfigPath(name))
	b.Logger.Debug("windows remove completed", "service_name", name)
	return nil
}

func (b *Backend) serviceNameOccupied(ctx context.Context, name string) (bool, error) {
	res, err := b.Runner.Run(ctx, "wireguard.exe", "/dumptunnels")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if strings.EqualFold(strings.TrimSpace(line), name) {
			return true, nil
		}
	}
	return false, nil
}
