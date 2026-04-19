package wgquick

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/execx"
	"github.com/exeteres/wg-feed/internal/client/linuxcmd"
	"github.com/exeteres/wg-feed/internal/client/namegen"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/client/wgquick"
)

type Backend struct {
	runner execx.Runner
	logger *slog.Logger
}

type tunnelData struct {
	DeviceName string `json:"device_name"`
}

func New(runner execx.Runner, logger *slog.Logger) *Backend {
	return &Backend{runner: runner, logger: logger}
}

func (b *Backend) Apply(ctx context.Context, tunnel shared.ResolvedTunnel, state state.TunnelState) (state.TunnelState, error) {
	b.logger.Debug("wgquick apply started", "tunnel_id", tunnel.ID, "tunnel_name", tunnel.Name, "enabled", tunnel.EffectiveEnabled)
	enabled := tunnel.EffectiveEnabled
	tunnelData := tunnelData{}
	if len(state.Data) > 0 {
		_ = json.Unmarshal(state.Data, &tunnelData)
	}
	if !enabled {
		iface := strings.TrimSpace(tunnelData.DeviceName)
		if iface != "" {
			b.logger.Debug("wgquick down interface", "iface", iface, "tool", "awg-quick")
			_, _ = b.runner.Run(ctx, "awg-quick", "down", iface)
			b.logger.Debug("wgquick down interface", "iface", iface, "tool", "wg-quick")
			_, _ = b.runner.Run(ctx, "wg-quick", "down", iface)
		}
		state.Data = nil
		b.logger.Debug("wgquick apply disabled completed", "tunnel_id", tunnel.ID)
		return state, nil
	}

	iface := strings.TrimSpace(tunnelData.DeviceName)
	if iface == "" {
		effective, err := namegen.EffectiveName(strings.TrimSpace(tunnel.Name), func(candidate string) (bool, error) {
			return b.interfaceOccupied(ctx, candidate)
		})
		if err != nil {
			return state, err
		}
		iface = effective
	}
	if iface == "" {
		return state, errors.New("wg-quick backend requires a non-empty tunnel name")
	}
	tunnelData.DeviceName = iface
	dataBytes, err := json.Marshal(tunnelData)
	if err != nil {
		return state, err
	}
	wgQuickConfig := tunnel.WGQuickConfig
	if !strings.HasSuffix(wgQuickConfig, "\n") {
		wgQuickConfig += "\n"
	}
	commands := linuxcmd.CommandSetForConfig(wgQuickConfig)

	tmpDir, err := os.MkdirTemp("", "wg-feed-*")
	if err != nil {
		return state, err
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	configPath := filepath.Join(tmpDir, iface+".conf")
	if err := os.WriteFile(configPath, []byte(wgQuickConfig), 0o600); err != nil {
		return state, err
	}

	if linuxcmd.IsUp(ctx, b.runner, iface, commands) {
		if ok := bestEffortDeviceUpdate(ctx, b, configPath, iface, commands); ok {
			state.Data = dataBytes
			return state, nil
		}
	}

	// Fall back to wg-quick (down/up) when interface isn't up or device update fails.
	b.logger.Debug("wgquick down interface before up", "iface", iface, "tool", commands.WGQuick)
	_, _ = b.runner.Run(ctx, commands.WGQuick, "down", iface)
	b.logger.Debug("wgquick up interface", "iface", iface, "tool", commands.WGQuick, "config_path", configPath)
	if _, err := b.runner.Run(ctx, commands.WGQuick, "up", configPath); err != nil {
		return state, err
	}
	state.Data = dataBytes
	b.logger.Debug("wgquick apply completed", "tunnel_id", tunnel.ID, "iface", iface)
	return state, nil
}

func (b *Backend) Remove(ctx context.Context, state state.TunnelState) error {
	tunnelData := tunnelData{}
	_ = json.Unmarshal(state.Data, &tunnelData)
	iface := strings.TrimSpace(tunnelData.DeviceName)
	if iface == "" {
		return nil
	}
	b.logger.Debug("wgquick remove down interface", "iface", iface, "tool", "awg-quick")
	_, _ = b.runner.Run(ctx, "awg-quick", "down", iface)
	b.logger.Debug("wgquick remove down interface", "iface", iface, "tool", "wg-quick")
	_, _ = b.runner.Run(ctx, "wg-quick", "down", iface)
	b.logger.Debug("wgquick remove completed", "iface", iface)
	return nil
}

func (b *Backend) interfaceOccupied(ctx context.Context, iface string) (bool, error) {
	res, err := b.runner.Run(ctx, "ip", "-o", "link", "show", "dev", iface)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(res.Stdout) != "", nil
}

func bestEffortDeviceUpdate(ctx context.Context, b *Backend, configPath string, iface string, commands linuxcmd.CommandSet) bool {
	oldAllowed, err := currentAllowedIPPrefixes(ctx, b.runner, iface, commands)
	if err != nil {
		b.logger.Debug("wg showconf failed", "iface", iface, "err", err)
		oldAllowed = nil
	}

	stripRes, err := b.runner.Run(ctx, commands.WGQuick, "strip", configPath)
	if err != nil {
		b.logger.Debug("wg-quick strip failed", "iface", iface, "err", err)
		return false
	}
	stripped := strings.TrimSpace(stripRes.Stdout)
	if stripped == "" {
		b.logger.Debug("wg-quick strip returned empty config", "iface", iface)
		return false
	}

	newAllowed, err := wgquick.AllowedIPPrefixSetFromText(stripped)
	if err != nil {
		b.logger.Debug("parse stripped config failed", "iface", iface, "err", err)
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
	b.logger.Debug("wgquick syncconf", "iface", iface, "config_path", tmp, "tool", commands.WG)
	if _, err := b.runner.Run(ctx, commands.WG, "syncconf", iface, tmp); err == nil {
		if err := reconcileAllowedIPRoutes(ctx, b, iface, oldAllowed, newAllowed); err != nil {
			b.logger.Debug("route reconciliation failed after syncconf", "iface", iface, "err", err)
			return false
		}
		return true
	} else {
		b.logger.Debug("wg syncconf failed", "iface", iface, "err", err)
	}
	b.logger.Debug("wgquick setconf", "iface", iface, "config_path", tmp, "tool", commands.WG)
	if _, err := b.runner.Run(ctx, commands.WG, "setconf", iface, tmp); err == nil {
		if err := reconcileAllowedIPRoutes(ctx, b, iface, oldAllowed, newAllowed); err != nil {
			b.logger.Debug("route reconciliation failed after setconf", "iface", iface, "err", err)
			return false
		}
		return true
	} else {
		b.logger.Debug("wg setconf failed", "iface", iface, "err", err)
	}
	return false
}

func currentAllowedIPPrefixes(ctx context.Context, runner execx.Runner, iface string, commands linuxcmd.CommandSet) (map[netip.Prefix]struct{}, error) {
	res, err := runner.Run(ctx, commands.WG, "showconf", iface)
	if err != nil {
		return nil, err
	}
	return wgquick.AllowedIPPrefixSetFromText(res.Stdout)
}

func reconcileAllowedIPRoutes(ctx context.Context, b *Backend, iface string, oldAllowed, newAllowed map[netip.Prefix]struct{}) error {
	// If we couldn't determine old state, only ensure new routes exist.
	if oldAllowed == nil {
		b.logger.Debug("wgquick reconcile allowedips routes", "iface", iface, "replace_count", len(newAllowed), "delete_count", 0)
		for p := range newAllowed {
			if err := linuxcmd.RouteReplace(ctx, b.runner, iface, p, ""); err != nil {
				return err
			}
		}
		return nil
	}

	toAdd := make([]netip.Prefix, 0)
	toDel := make([]netip.Prefix, 0)
	for p := range newAllowed {
		if _, ok := oldAllowed[p]; !ok {
			toAdd = append(toAdd, p)
		}
	}
	for p := range oldAllowed {
		if _, ok := newAllowed[p]; !ok {
			toDel = append(toDel, p)
		}
	}

	// Deterministic order helps tests/logs.
	slices.SortFunc(toAdd, func(a, b netip.Prefix) int { return strings.Compare(a.String(), b.String()) })
	slices.SortFunc(toDel, func(a, b netip.Prefix) int { return strings.Compare(a.String(), b.String()) })
	b.logger.Debug("wgquick reconcile allowedips routes", "iface", iface, "replace_count", len(toAdd), "delete_count", len(toDel))

	// Add first, then delete.
	for _, p := range toAdd {
		if err := linuxcmd.RouteReplace(ctx, b.runner, iface, p, ""); err != nil {
			return err
		}
	}
	for _, p := range toDel {
		if err := linuxcmd.RouteDelete(ctx, b.runner, iface, p, ""); err != nil {
			// Best-effort: deletions may race with external tools.
			b.logger.Debug("ip route del failed", "iface", iface, "prefix", p.String(), "err", err)
		}
	}
	return nil
}
