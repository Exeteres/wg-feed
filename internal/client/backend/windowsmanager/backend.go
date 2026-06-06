package windowsmanager

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/namegen"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/client/wgquick"
)

type Backend struct {
	Logger *slog.Logger
}

type tunnelData struct {
	ConfigName string `json:"config_name"`
	Amnezia    bool   `json:"amnezia,omitempty"`
}

func New(logger *slog.Logger) *Backend {
	return &Backend{Logger: logger}
}

func resolveProgramFilesDir() string {
	base := strings.TrimSpace(os.Getenv("ProgramFiles"))
	if base == "" {
		base = `C:\Program Files`
	}
	return base
}

func managerConfigDir(amnezia bool) string {
	base := resolveProgramFilesDir()
	if amnezia {
		return filepath.Join(base, "AmneziaWG", "Data", "Configurations")
	}
	return filepath.Join(base, "WireGuard", "Data", "Configurations")
}

func managerConfPath(name string, amnezia bool) string {
	return filepath.Join(managerConfigDir(amnezia), name+".conf")
}

func managerDPAPIPath(name string, amnezia bool) string {
	return filepath.Join(managerConfigDir(amnezia), name+".conf.dpapi")
}

func (b *Backend) Apply(_ context.Context, tunnel shared.ResolvedTunnel, st state.TunnelState) (state.TunnelState, error) {
	b.Logger.Debug("windows-manager apply started", "tunnel_id", tunnel.ID, "tunnel_name", tunnel.Name, "enabled", tunnel.EffectiveEnabled)

	amnezia := wgquick.HasAmneziaExtensions(tunnel.WGQuickConfig)
	prev := tunnelData{}
	if len(st.Data) > 0 {
		_ = json.Unmarshal(st.Data, &prev)
	}

	name := strings.TrimSpace(prev.ConfigName)
	if name == "" {
		effective, err := namegen.EffectiveName(strings.TrimSpace(tunnel.Name), func(candidate string) (bool, error) {
			return configNameOccupied(candidate, amnezia), nil
		})
		if err != nil {
			return st, err
		}
		name = strings.TrimSpace(effective)
	}
	if name == "" {
		return st, errors.New("windows-manager backend requires a non-empty tunnel name")
	}

	next := tunnelData{ConfigName: name, Amnezia: amnezia}
	data, err := json.Marshal(next)
	if err != nil {
		return st, err
	}
	st.Data = data

	if prevName := strings.TrimSpace(prev.ConfigName); prevName != "" {
		removeManagedConfig(prevName, prev.Amnezia)
	}
	removeManagedConfig(name, amnezia)

	if !tunnel.EffectiveEnabled {
		b.Logger.Debug("windows-manager apply disabled completed", "config_name", name, "amnezia", amnezia)
		return st, nil
	}

	wgQuickConfig := tunnel.WGQuickConfig
	if !strings.HasSuffix(wgQuickConfig, "\n") {
		wgQuickConfig += "\n"
	}
	confPath := managerConfPath(name, amnezia)
	if err := os.MkdirAll(filepath.Dir(confPath), 0o700); err != nil {
		return st, err
	}
	if err := os.WriteFile(confPath, []byte(wgQuickConfig), 0o600); err != nil {
		return st, err
	}
	b.Logger.Debug("windows-manager config staged", "config_name", name, "config_path", confPath, "amnezia", amnezia)
	return st, nil
}

func (b *Backend) Remove(_ context.Context, st state.TunnelState) error {
	td := tunnelData{}
	_ = json.Unmarshal(st.Data, &td)
	name := strings.TrimSpace(td.ConfigName)
	if name == "" {
		return nil
	}
	removeManagedConfig(name, td.Amnezia)
	b.Logger.Debug("windows-manager remove completed", "config_name", name, "amnezia", td.Amnezia)
	return nil
}

func configNameOccupied(name string, amnezia bool) bool {
	if _, err := os.Stat(managerDPAPIPath(name, amnezia)); err == nil {
		return true
	}
	if _, err := os.Stat(managerConfPath(name, amnezia)); err == nil {
		return true
	}
	return false
}

func removeManagedConfig(name string, amnezia bool) {
	_ = os.Remove(managerDPAPIPath(name, amnezia))
	_ = os.Remove(managerConfPath(name, amnezia))
}
