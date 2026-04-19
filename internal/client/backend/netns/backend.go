package netns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/execx"
	"github.com/exeteres/wg-feed/internal/client/linuxcmd"
	"github.com/exeteres/wg-feed/internal/client/namegen"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/client/wgquick"
)

type Backend struct {
	runner    execx.Runner
	logger    *slog.Logger
	netnsEtc  string
	mkdirAll  func(string, os.FileMode) error
	writeFile func(string, []byte, os.FileMode) error
	remove    func(string) error
}

type tunnelData struct {
	DeviceName string `json:"device_name"`
	Namespace  string `json:"namespace"`
}

func New(runner execx.Runner, logger *slog.Logger) *Backend {
	return &Backend{
		runner:    runner,
		logger:    logger,
		netnsEtc:  "/etc/netns",
		mkdirAll:  os.MkdirAll,
		writeFile: os.WriteFile,
		remove:    os.Remove,
	}
}

func (b *Backend) Apply(ctx context.Context, tunnel shared.ResolvedTunnel, state state.TunnelState) (state.TunnelState, error) {
	b.logger.Debug("netns apply started", "tunnel_id", tunnel.ID, "tunnel_name", tunnel.Name, "enabled", tunnel.EffectiveEnabled)

	td := decodeTunnelData(state.Data)

	if !tunnel.EffectiveEnabled {
		if err := b.teardown(ctx, td); err != nil {
			b.logger.Debug("netns teardown failed on disabled apply", "tunnel_id", tunnel.ID, "device", td.DeviceName, "namespace", td.Namespace, "err", err)
			return state, err
		}
		state.Data = nil
		b.logger.Debug("netns apply disabled completed", "tunnel_id", tunnel.ID)
		return state, nil
	}

	resolved, err := b.resolveNames(ctx, tunnel, td)
	if err != nil {
		return state, err
	}
	td = resolved
	b.logger.Debug("netns lock names", "tunnel_id", tunnel.ID, "device", td.DeviceName, "namespace", td.Namespace)
	dataBytes, err := json.Marshal(td)
	if err != nil {
		return state, err
	}
	state.Data = dataBytes

	cfg, err := wgquick.Parse([]byte(tunnel.WGQuickConfig))
	if err != nil {
		return state, fmt.Errorf("parse wg-quick config: %w", err)
	}
	if err := wgquick.ValidateRequired(cfg); err != nil {
		return state, err
	}

	commands := linuxcmd.CommandSetForConfig(tunnel.WGQuickConfig)
	if err := b.ensureNamespace(ctx, td.Namespace); err != nil {
		b.logger.Debug("netns ensure namespace failed", "tunnel_id", tunnel.ID, "device", td.DeviceName, "namespace", td.Namespace, "err", err)
		return state, err
	}
	if err := b.ensureInterfaceInTargetNamespace(ctx, td, commands); err != nil {
		b.logger.Debug("netns move or reuse existing interface failed", "tunnel_id", tunnel.ID, "device", td.DeviceName, "namespace", td.Namespace, "err", err)
		return state, err
	}
	if err := b.applyDeviceConfig(ctx, td, tunnel.WGQuickConfig, commands); err != nil {
		b.logger.Debug("netns apply device config failed", "tunnel_id", tunnel.ID, "device", td.DeviceName, "namespace", td.Namespace, "err", err)
		return state, err
	}
	if err := b.configureNamespaceNetwork(ctx, td, cfg); err != nil {
		b.logger.Debug("netns namespace network configure failed", "tunnel_id", tunnel.ID, "device", td.DeviceName, "namespace", td.Namespace, "err", err)
		return state, err
	}
	if err := b.syncNamespaceResolvConf(td.Namespace, cfg.Interface.DNS); err != nil {
		b.logger.Debug("netns resolv.conf update failed", "tunnel_id", tunnel.ID, "namespace", td.Namespace, "err", err)
		return state, err
	}

	b.logger.Debug("netns apply completed", "tunnel_id", tunnel.ID, "device", td.DeviceName, "namespace", td.Namespace)
	return state, nil
}

func (b *Backend) Remove(ctx context.Context, state state.TunnelState) error {
	td := decodeTunnelData(state.Data)
	b.logger.Debug("netns remove started", "device", td.DeviceName, "namespace", td.Namespace)
	return b.teardown(ctx, td)
}

func decodeTunnelData(data []byte) tunnelData {
	td := tunnelData{}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &td)
	}
	return sanitizeTunnelData(td)
}

func sanitizeTunnelData(td tunnelData) tunnelData {
	return tunnelData{
		DeviceName: strings.TrimSpace(td.DeviceName),
		Namespace:  strings.TrimSpace(td.Namespace),
	}
}

func (b *Backend) resolveNames(ctx context.Context, tunnel shared.ResolvedTunnel, td tunnelData) (tunnelData, error) {
	deviceName := td.DeviceName
	namespace := td.Namespace
	if deviceName == "" {
		requested := strings.TrimSpace(tunnel.Name)
		if requested == "" {
			return tunnelData{}, errors.New("netns backend requires a non-empty tunnel name")
		}

		// Reclaim base names when resources already exist, so retries don't keep
		// allocating -1, -2, ... after partial failures.
		namespaceHint := namespace
		if namespaceHint == "" {
			namespaceHint = requested
		}
		inInit, _ := b.interfaceExistsInInitNamespace(ctx, requested)
		inTarget, _ := b.interfaceExistsInNamespace(ctx, namespaceHint, requested)
		if inInit || inTarget || b.namespaceExists(ctx, namespaceHint) {
			deviceName = requested
		} else {
			effective, err := namegen.EffectiveName(requested, func(candidate string) (bool, error) {
				return b.nameOccupied(ctx, candidate)
			})
			if err != nil {
				return tunnelData{}, err
			}
			deviceName = effective
		}
	}
	if deviceName == "" {
		return tunnelData{}, errors.New("netns backend requires a non-empty tunnel name")
	}
	if namespace == "" {
		namespace = deviceName
	}
	return tunnelData{DeviceName: deviceName, Namespace: namespace}, nil
}

func (b *Backend) nameOccupied(ctx context.Context, name string) (bool, error) {
	res, err := b.runner.Run(ctx, "ip", "-o", "link", "show", "dev", name)
	if err == nil && strings.TrimSpace(res.Stdout) != "" {
		return true, nil
	}

	res, err = b.runner.Run(ctx, "ip", "netns", "list")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		if fields[0] == name {
			return true, nil
		}
	}
	return false, nil
}

func (b *Backend) ensureNamespace(ctx context.Context, namespace string) error {
	if namespace == "" {
		return nil
	}
	if b.namespaceExists(ctx, namespace) {
		return nil
	}
	b.logger.Debug("netns create namespace", "namespace", namespace)
	_, err := b.runner.Run(ctx, "ip", "netns", "add", namespace)
	return err
}

func (b *Backend) ensureInterfaceInTargetNamespace(ctx context.Context, td tunnelData, commands linuxcmd.CommandSet) error {
	inTargetNs, err := b.interfaceExistsInNamespace(ctx, td.Namespace, td.DeviceName)
	if err != nil {
		return err
	}
	if inTargetNs {
		return nil
	}

	inInitNs, err := b.interfaceExistsInInitNamespace(ctx, td.DeviceName)
	if err != nil {
		return err
	}
	if !inInitNs {
		b.logger.Debug("netns create interface in init namespace", "device", td.DeviceName, "link_type", commands.LinkType)
		if _, err := b.runner.Run(ctx, "ip", "link", "add", "dev", td.DeviceName, "type", commands.LinkType); err != nil {
			return err
		}
	}

	b.logger.Debug("netns move interface to namespace", "device", td.DeviceName, "namespace", td.Namespace)
	if _, err := b.runner.Run(ctx, "ip", "link", "set", "dev", td.DeviceName, "netns", td.Namespace); err != nil {
		return err
	}
	return nil
}

func (b *Backend) applyDeviceConfig(ctx context.Context, td tunnelData, wgQuickConfig string, commands linuxcmd.CommandSet) error {
	tmpDir, err := os.MkdirTemp("", "wg-feed-netns-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	fullPath := filepath.Join(tmpDir, td.DeviceName+".conf")
	full := wgQuickConfig
	if !strings.HasSuffix(full, "\n") {
		full += "\n"
	}
	if err := os.WriteFile(fullPath, []byte(full), 0o600); err != nil {
		return err
	}

	stripRes, err := b.runner.Run(ctx, commands.WGQuick, "strip", fullPath)
	if err != nil {
		return err
	}
	stripped := strings.TrimSpace(stripRes.Stdout)
	if stripped == "" {
		return errors.New("wg-quick strip returned empty config")
	}
	stripPath := filepath.Join(tmpDir, td.DeviceName+".stripped.conf")
	if err := os.WriteFile(stripPath, []byte(stripped+"\n"), 0o600); err != nil {
		return err
	}

	b.logger.Debug("netns apply wg setconf", "device", td.DeviceName, "namespace", td.Namespace, "config_path", stripPath)
	if _, err := b.runner.Run(ctx, "ip", "netns", "exec", td.Namespace, commands.WG, "setconf", td.DeviceName, stripPath); err != nil {
		return err
	}
	return nil
}

func (b *Backend) interfaceExistsInInitNamespace(ctx context.Context, iface string) (bool, error) {
	_, err := b.runner.Run(ctx, "ip", "-o", "link", "show", "dev", iface)
	if err == nil {
		return true, nil
	}
	if isMissingResourceError(err) {
		return false, nil
	}
	return false, fmt.Errorf("check interface in init namespace %q: %w", iface, err)
}

func (b *Backend) interfaceExistsInNamespace(ctx context.Context, netns string, iface string) (bool, error) {
	_, err := b.runner.Run(ctx, "ip", "-n", netns, "-o", "link", "show", "dev", iface)
	if err == nil {
		return true, nil
	}
	return false, nil
}

func (b *Backend) configureNamespaceNetwork(ctx context.Context, td tunnelData, cfg wgquick.Config) error {
	if cfg.Interface.MTU != nil {
		b.logger.Debug("netns set interface mtu", "device", td.DeviceName, "namespace", td.Namespace, "mtu", *cfg.Interface.MTU)
		if _, err := b.runner.Run(ctx, "ip", "-n", td.Namespace, "link", "set", "dev", td.DeviceName, "mtu", fmt.Sprintf("%d", *cfg.Interface.MTU)); err != nil {
			return err
		}
	}

	for _, addr := range cfg.Interface.Addresses {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		b.logger.Debug("netns replace interface address", "device", td.DeviceName, "namespace", td.Namespace, "address", addr)
		if _, err := b.runner.Run(ctx, "ip", "-n", td.Namespace, "address", "replace", addr, "dev", td.DeviceName); err != nil {
			return err
		}
	}

	b.logger.Debug("netns set interface up", "device", td.DeviceName, "namespace", td.Namespace)
	if _, err := b.runner.Run(ctx, "ip", "-n", td.Namespace, "link", "set", "dev", td.DeviceName, "up"); err != nil {
		return err
	}

	routes := wgquick.AllowedIPPrefixes(cfg)
	b.logger.Debug("netns reconcile allowedips routes", "device", td.DeviceName, "namespace", td.Namespace, "replace_count", len(routes))
	for _, p := range routes {
		if err := linuxcmd.RouteReplace(ctx, b.runner, td.DeviceName, p, td.Namespace); err != nil {
			return err
		}
	}
	return nil
}

func (b *Backend) teardown(ctx context.Context, td tunnelData) error {
	if td.DeviceName == "" && td.Namespace == "" {
		return nil
	}
	var errs []error
	if td.DeviceName != "" {
		b.logger.Debug("netns delete interface in namespace", "device", td.DeviceName, "namespace", td.Namespace)
		if err := b.deleteInterfaceInNamespace(ctx, td.Namespace, td.DeviceName); err != nil && !isMissingResourceError(err) {
			errs = append(errs, fmt.Errorf("delete interface %q in namespace %q: %w", td.DeviceName, td.Namespace, err))
		}
		b.logger.Debug("netns delete interface in init namespace", "device", td.DeviceName)
		if _, err := b.runner.Run(ctx, "ip", "link", "del", "dev", td.DeviceName); err != nil && !isMissingResourceError(err) {
			errs = append(errs, fmt.Errorf("delete interface %q in init namespace: %w", td.DeviceName, err))
		}
	}
	b.logger.Debug("netns delete namespace", "namespace", td.Namespace)
	if _, err := b.runner.Run(ctx, "ip", "netns", "del", td.Namespace); err != nil && !isMissingResourceError(err) {
		errs = append(errs, fmt.Errorf("delete namespace %q: %w", td.Namespace, err))
	}
	if err := b.cleanupNamespaceResolvConf(td.Namespace); err != nil && !isMissingResourceError(err) {
		errs = append(errs, fmt.Errorf("remove namespace resolv.conf for %q: %w", td.Namespace, err))
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (b *Backend) namespaceResolvConfPath(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return ""
	}
	return filepath.Join(b.netnsEtc, namespace, "resolv.conf")
}

func (b *Backend) cleanupNamespaceResolvConf(namespace string) error {
	path := b.namespaceResolvConfPath(namespace)
	if path == "" {
		return nil
	}
	return b.remove(path)
}

func (b *Backend) syncNamespaceResolvConf(namespace string, dns []string) error {
	path := b.namespaceResolvConfPath(namespace)
	if path == "" {
		return nil
	}

	lines := make([]string, 0, len(dns))
	seen := map[string]struct{}{}
	for _, entry := range dns {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		lines = append(lines, "nameserver "+entry)
	}
	if len(lines) == 0 {
		if err := b.remove(path); err != nil && !isMissingResourceError(err) {
			return err
		}
		return nil
	}

	if err := b.mkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := strings.Join(lines, "\n") + "\n"
	return b.writeFile(path, []byte(content), 0o644)
}

func (b *Backend) deleteInterfaceInNamespace(ctx context.Context, netns string, iface string) error {
	if netns == "" || iface == "" {
		return nil
	}
	_, err := b.runner.Run(ctx, "ip", "-n", netns, "link", "del", "dev", iface)
	return err
}

func isMissingResourceError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no such file or directory") ||
		strings.Contains(s, "not found") ||
		strings.Contains(s, "does not exist") ||
		strings.Contains(s, "cannot find device") ||
		strings.Contains(s, "cannot open network namespace") ||
		strings.Contains(s, "cannot remove namespace file")
}

func (b *Backend) namespaceExists(ctx context.Context, namespace string) bool {
	res, err := b.runner.Run(ctx, "ip", "netns", "list")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		if fields[0] == namespace {
			return true
		}
	}
	return false
}
