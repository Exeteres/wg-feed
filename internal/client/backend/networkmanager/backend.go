package networkmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/backend/networkmanager/nmconfig"
	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/execx"
	"github.com/exeteres/wg-feed/internal/client/namegen"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/client/wgquick"
	"github.com/google/uuid"
)

type provisioningMode struct {
	amnezia bool
}

type Backend struct {
	runner     execx.Runner
	logger     *slog.Logger
	nmDir      string
	listPath   string
	scriptPath string
	read       func(string) ([]byte, error)
	write      func(string, []byte, os.FileMode) error
	mkdirAll   func(string, os.FileMode) error
	remove     func(string) error
	uuidGen    func() string
}

type tunnelData struct {
	ConnectionID   string `json:"connection_id"`
	ConnectionUUID string `json:"connection_uuid"`
}

func New(runner execx.Runner, logger *slog.Logger) *Backend {
	return &Backend{
		runner:     runner,
		logger:     logger,
		nmDir:      "/etc/NetworkManager/system-connections",
		listPath:   "/etc/NetworkManager/system-connections/.wg-feed-exclusive-connections",
		scriptPath: "/etc/NetworkManager/dispatcher.d/90-wg-feed-exclusive",
		read:       os.ReadFile,
		write:      os.WriteFile,
		mkdirAll:   os.MkdirAll,
		remove:     os.Remove,
		uuidGen:    uuid.NewString,
	}
}

func (b *Backend) Apply(ctx context.Context, tunnel shared.ResolvedTunnel, state state.TunnelState) (state.TunnelState, error) {
	b.logger.Debug("networkmanager apply started", "tunnel_id", tunnel.ID, "tunnel_name", tunnel.Name, "enabled", tunnel.Enabled, "effective_enabled", tunnel.EffectiveEnabled, "exclusive", tunnel.Exclusive)
	effectiveEnabled := tunnel.EffectiveEnabled

	parsed, err := wgquick.Parse([]byte(tunnel.WGQuickConfig))
	if err != nil {
		return state, fmt.Errorf("parse wg-quick config: %w", err)
	}
	if err := wgquick.ValidateRequired(parsed); err != nil {
		return state, err
	}

	tunnelData := tunnelData{}
	if len(state.Data) > 0 {
		_ = json.Unmarshal(state.Data, &tunnelData)
	}
	name := strings.TrimSpace(tunnelData.ConnectionID)
	if name == "" {
		effective, err := namegen.EffectiveName(strings.TrimSpace(tunnel.Name), func(candidate string) (bool, error) {
			return b.connectionNameOccupied(ctx, candidate)
		})
		if err != nil {
			return state, err
		}
		name = effective
	}
	if strings.TrimSpace(name) == "" {
		return state, errors.New("networkmanager backend requires a non-empty connection name")
	}
	tunnelData.ConnectionID = strings.TrimSpace(name)
	if strings.TrimSpace(tunnelData.ConnectionUUID) == "" {
		tunnelData.ConnectionUUID = b.uuidGen()
	}
	dataBytes, err := json.Marshal(tunnelData)
	if err != nil {
		return state, err
	}
	state.Data = dataBytes

	mode := modeForConfig(tunnel.WGQuickConfig)
	autoconnect := effectiveEnabled
	nmPath := b.nmConnectionPath(name)

	out, buildErr := buildNMConnection(name, parsed, mode, autoconnect, tunnelData.ConnectionUUID)
	if buildErr != nil {
		return state, buildErr
	}
	if err := b.mkdirAll(filepath.Dir(nmPath), 0o755); err != nil {
		return state, fmt.Errorf("mkdir nm dir: %w", err)
	}
	b.logger.Debug("networkmanager write connection file", "connection_id", name, "path", nmPath)
	if err := b.write(nmPath, out, 0o600); err != nil {
		return state, fmt.Errorf("write nmconnection: %w", err)
	}

	b.logger.Debug("networkmanager sync exclusive registration", "connection_id", name, "exclusive", tunnel.Exclusive)
	if err := b.syncExclusiveRegistration(name, tunnel.Exclusive); err != nil {
		return state, fmt.Errorf("sync exclusive registration: %w", err)
	}

	b.logger.Debug("networkmanager nmcli reload connections")
	_, _ = b.runner.Run(ctx, "nmcli", "connection", "reload")
	if effectiveEnabled {
		b.logger.Debug("networkmanager nmcli connection up", "connection_id", name)
		_, err = b.runner.Run(ctx, "nmcli", "connection", "up", "id", name)
		if err == nil {
			b.logger.Debug("networkmanager apply completed", "connection_id", name, "enabled", true)
		}
		return state, err
	}
	if !shouldBringDownDisabledConnection(tunnel) {
		b.logger.Debug("networkmanager skip connection down for disabled tunnel with forced=false", "connection_id", name)
		b.logger.Debug("networkmanager apply completed", "connection_id", name, "enabled", false)
		return state, nil
	}
	b.logger.Debug("networkmanager nmcli connection down", "connection_id", name)
	_, err = b.runner.Run(ctx, "nmcli", "connection", "down", "id", name)
	if err != nil && isNMCLIExitCode(err, 10) {
		b.logger.Debug("networkmanager apply completed (connection already down)", "connection_id", name)
		return state, nil
	}
	if err == nil {
		b.logger.Debug("networkmanager apply completed", "connection_id", name, "enabled", false)
	}
	return state, err
}

func shouldBringDownDisabledConnection(tunnel shared.ResolvedTunnel) bool {
	if tunnel.Forced && !tunnel.Enabled {
		return true
	}
	return tunnel.Enabled != tunnel.EffectiveEnabled
}

func modeForConfig(wgQuickConfig string) provisioningMode {
	if wgquick.HasAmneziaExtensions(wgQuickConfig) {
		return provisioningMode{amnezia: true}
	}
	return provisioningMode{}
}

func isNMCLIExitCode(err error, code int) bool {
	if err == nil {
		return false
	}
	// exec.ExitError (and compatible types) implement ExitCode().
	var ec interface{ ExitCode() int }
	if errors.As(err, &ec) {
		return ec.ExitCode() == code
	}
	return false
}

func (b *Backend) Remove(ctx context.Context, state state.TunnelState) error {
	tunnelData := tunnelData{}
	_ = json.Unmarshal(state.Data, &tunnelData)
	name := strings.TrimSpace(tunnelData.ConnectionID)
	if name == "" {
		return nil
	}
	b.logger.Debug("networkmanager remove started", "connection_id", name)
	var firstErr error
	b.logger.Debug("networkmanager sync exclusive registration", "connection_id", name, "exclusive", false)
	if err := b.syncExclusiveRegistration(name, false); err != nil {
		firstErr = err
		b.logger.Debug("sync exclusive registration failed on remove", "name", name, "err", err)
	}
	b.logger.Debug("networkmanager nmcli connection down", "connection_id", name)
	_, _ = b.runner.Run(ctx, "nmcli", "connection", "down", "id", name)
	b.logger.Debug("networkmanager nmcli connection delete", "connection_id", name)
	_, _ = b.runner.Run(ctx, "nmcli", "connection", "delete", "id", name)
	b.logger.Debug("networkmanager remove connection file", "connection_id", name, "path", b.nmConnectionPath(name))
	_ = b.remove(b.nmConnectionPath(name))
	b.logger.Debug("networkmanager remove completed", "connection_id", name)
	return firstErr
}

func (b *Backend) connectionNameOccupied(ctx context.Context, name string) (bool, error) {
	res, err := b.runner.Run(ctx, "nmcli", "-g", "NAME", "connection", "show", "id", name)
	if err == nil && strings.TrimSpace(res.Stdout) != "" {
		return true, nil
	}
	_, statErr := os.Stat(b.nmConnectionPath(name))
	if statErr == nil {
		return true, nil
	}
	if errors.Is(statErr, os.ErrNotExist) {
		return false, nil
	}
	return false, nil
}

func (b *Backend) syncExclusiveRegistration(name string, exclusive bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	names, err := b.readExclusiveNames()
	if err != nil {
		return err
	}
	if exclusive {
		b.logger.Debug("networkmanager mark connection exclusive", "connection_id", name)
		names[name] = struct{}{}
	} else {
		b.logger.Debug("networkmanager unmark connection exclusive", "connection_id", name)
		delete(names, name)
	}
	return b.writeExclusiveState(names)
}

func (b *Backend) readExclusiveNames() (map[string]struct{}, error) {
	out := map[string]struct{}{}
	bts, err := b.read(b.listPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(bts), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out[line] = struct{}{}
	}
	return out, nil
}

func (b *Backend) writeExclusiveState(names map[string]struct{}) error {
	if len(names) == 0 {
		b.logger.Debug("networkmanager remove exclusive registry files", "list_path", b.listPath, "script_path", b.scriptPath)
		_ = b.remove(b.listPath)
		_ = b.remove(b.scriptPath)
		return nil
	}

	entries := make([]string, 0, len(names))
	for name := range names {
		entries = append(entries, name)
	}
	sort.Strings(entries)
	listContent := strings.Join(entries, "\n") + "\n"

	b.logger.Debug("networkmanager ensure exclusive list dir", "dir", filepath.Dir(b.listPath))
	if err := b.mkdirAll(filepath.Dir(b.listPath), 0o755); err != nil {
		return err
	}
	b.logger.Debug("networkmanager ensure exclusive dispatcher dir", "dir", filepath.Dir(b.scriptPath))
	if err := b.mkdirAll(filepath.Dir(b.scriptPath), 0o755); err != nil {
		return err
	}
	b.logger.Debug("networkmanager write exclusive list", "path", b.listPath, "connections", entries)
	if err := b.write(b.listPath, []byte(listContent), 0o600); err != nil {
		return err
	}
	b.logger.Debug("networkmanager write exclusive dispatcher script", "path", b.scriptPath)
	if err := b.write(b.scriptPath, []byte(exclusiveDispatcherScript(b.listPath)), 0o755); err != nil {
		return err
	}
	return nil
}

func exclusiveDispatcherScript(listPath string) string {
	return fmt.Sprintf(`#!/bin/sh
# Managed by wg-feed. Enforces single-active behavior for exclusive tunnels.
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/run/current-system/sw/bin
LIST_FILE=%q
EVENT="$2"
if [ "$EVENT" != "up" ] && [ "$EVENT" != "vpn-up" ]; then
  exit 0
fi
if [ -z "$CONNECTION_ID" ]; then
  exit 0
fi
if [ ! -r "$LIST_FILE" ]; then
  exit 0
fi
is_exclusive=0
while IFS= read -r name; do
  [ -z "$name" ] && continue
  if [ "$name" = "$CONNECTION_ID" ]; then
    is_exclusive=1
    break
  fi
done < "$LIST_FILE"
if [ "$is_exclusive" -ne 1 ]; then
  exit 0
fi
while IFS= read -r name; do
  [ -z "$name" ] && continue
  if [ "$name" = "$CONNECTION_ID" ]; then
    continue
  fi
  nmcli connection down id "$name" >/dev/null 2>&1 || true
done < "$LIST_FILE"
`, listPath)
}

func buildNMConnection(name string, parsed wgquick.Config, mode provisioningMode, autoconnect bool, connectionUUID string) ([]byte, error) {
	kf := nmconfig.NewEmpty()
	uuidVal := strings.TrimSpace(connectionUUID)
	if uuidVal == "" {
		uuidVal = uuid.NewString()
	}

	// [connection]
	kf.Set("connection", "id", name)
	kf.Set("connection", "uuid", uuidVal)
	if mode.amnezia {
		kf.Set("connection", "type", "vpn")
	} else {
		kf.Set("connection", "type", "wireguard")
		kf.Set("connection", "interface-name", name)
	}
	if autoconnect {
		kf.Set("connection", "autoconnect", "true")
	} else {
		kf.Set("connection", "autoconnect", "false")
	}

	if mode.amnezia {
		buildAmneziaVPNConnection(kf, parsed)
	} else {
		buildWireGuardConnection(kf, parsed)
	}

	return kf.Bytes(), nil
}

func buildWireGuardConnection(kf *nmconfig.File, parsed wgquick.Config) {
	// [wireguard]
	if parsed.Interface.MTU != nil {
		kf.Set("wireguard", "mtu", fmt.Sprintf("%d", *parsed.Interface.MTU))
	}
	kf.Set("wireguard", "private-key", parsed.Interface.PrivateKey)

	for _, p := range parsed.Peers {
		pk := strings.TrimSpace(p.PublicKey)
		if pk == "" {
			continue
		}
		sec := "wireguard-peer." + pk
		if p.Endpoint != "" {
			kf.Set(sec, "endpoint", p.Endpoint)
		}
		if p.PresharedKey != "" {
			kf.Set(sec, "preshared-key", p.PresharedKey)
			kf.Set(sec, "preshared-key-flags", "0")
		}
		if len(p.AllowedIPs) > 0 {
			kf.Set(sec, "allowed-ips", nmList(p.AllowedIPs))
		}
	}

	setIPConfig(kf, parsed.Interface)
}

func buildAmneziaVPNConnection(kf *nmconfig.File, parsed wgquick.Config) {
	kf.Set("vpn", "service-type", "org.freedesktop.NetworkManager.amneziawg")
	kf.Set("vpn-secrets", "local-private-key", parsed.Interface.PrivateKey)
	kf.Set("vpn", "local-private-key-flags", "0")

	if parsed.Interface.MTU != nil {
		kf.Set("vpn", "interface-mtu", fmt.Sprintf("%d", *parsed.Interface.MTU))
	}

	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "listenport", "local-listen-port")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "fwmark", "interface-fw-mark")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "jc", "connection-jc")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "jmin", "connection-jmin")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "jmax", "connection-jmax")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "s1", "connection-s1")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "s2", "connection-s2")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "s3", "connection-s3")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "s4", "connection-s4")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "h1", "connection-h1")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "h2", "connection-h2")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "h3", "connection-h3")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "h4", "connection-h4")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "i1", "connection-i1")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "i2", "connection-i2")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "i3", "connection-i3")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "i4", "connection-i4")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "i5", "connection-i5")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "preup", "script-pre-up")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "postup", "script-post-up")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "predown", "script-pre-down")
	setMappedExtra(kf, "vpn", parsed.Interface.Extras, "postdown", "script-post-down")

	for _, key := range []string{"connection-jc", "connection-jmin", "connection-jmax", "connection-s1", "connection-s2", "connection-s3", "connection-s4"} {
		if _, ok := kf.Get("vpn", key); !ok {
			kf.Set("vpn", key, "0")
		}
	}

	for i, p := range parsed.Peers {
		prefix := fmt.Sprintf("peer-%d-", i)
		if strings.TrimSpace(p.PublicKey) != "" {
			kf.Set("vpn", prefix+"public-key", strings.TrimSpace(p.PublicKey))
		}
		if strings.TrimSpace(p.Endpoint) != "" {
			kf.Set("vpn", prefix+"endpoint", strings.TrimSpace(p.Endpoint))
		}
		if len(p.AllowedIPs) > 0 {
			kf.Set("vpn", prefix+"allowed-ips", commaList(p.AllowedIPs))
		}
		if p.PersistentKeepalive != nil {
			kf.Set("vpn", prefix+"keep-alive", fmt.Sprintf("%d", *p.PersistentKeepalive))
		} else {
			kf.Set("vpn", prefix+"keep-alive", "0")
		}
		if p.PresharedKey != "" {
			kf.Set("vpn-secrets", prefix+"preshared-key", p.PresharedKey)
			kf.Set("vpn", prefix+"preshared-key-flags", "0")
		}
		if v, ok := p.Extras["advancedsecurity"]; ok {
			kf.Set("vpn", prefix+"advanced-security", v)
		} else {
			kf.Set("vpn", prefix+"advanced-security", "off")
		}
	}

	setIPConfig(kf, parsed.Interface)
	setRoutesFromAllowedIPs(kf, parsed.Peers)
}

func setRoutesFromAllowedIPs(kf *nmconfig.File, peers []wgquick.Peer) {
	seen4 := map[string]struct{}{}
	seen6 := map[string]struct{}{}
	var routes4 []string
	var routes6 []string

	for _, p := range peers {
		for _, allowed := range p.AllowedIPs {
			allowed = strings.TrimSpace(allowed)
			if allowed == "" {
				continue
			}
			if _, _, err := net.ParseCIDR(allowed); err != nil {
				continue
			}
			if strings.Contains(allowed, ":") {
				if _, ok := seen6[allowed]; ok {
					continue
				}
				seen6[allowed] = struct{}{}
				routes6 = append(routes6, allowed)
				continue
			}
			if _, ok := seen4[allowed]; ok {
				continue
			}
			seen4[allowed] = struct{}{}
			routes4 = append(routes4, allowed)
		}
	}

	for i, route := range routes4 {
		kf.Set("ipv4", fmt.Sprintf("route%d", i+1), route)
	}
	for i, route := range routes6 {
		kf.Set("ipv6", fmt.Sprintf("route%d", i+1), route)
	}
}

func setMappedExtra(kf *nmconfig.File, section string, extras map[string]string, sourceKey string, targetKey string) {
	v, ok := extras[sourceKey]
	if !ok {
		return
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	kf.Set(section, targetKey, v)
}

func setIPConfig(kf *nmconfig.File, iface wgquick.Interface) {
	ipv4Addrs, ipv6Addrs := splitIPs(iface.Addresses)
	if len(ipv4Addrs) > 0 {
		kf.Set("ipv4", "method", "manual")
		kf.Set("ipv4", "address1", ipv4Addrs[0])
	} else {
		kf.Set("ipv4", "method", "disabled")
	}
	if len(iface.DNS) > 0 {
		kf.Set("ipv4", "dns", nmList(iface.DNS))
		kf.Set("ipv4", "dns-search", "~;")
	}

	if len(ipv6Addrs) > 0 {
		kf.Set("ipv6", "method", "manual")
		kf.Set("ipv6", "address1", ipv6Addrs[0])
	} else {
		kf.Set("ipv6", "method", "disabled")
		kf.Set("ipv6", "addr-gen-mode", "default")
	}
}

func (b *Backend) nmConnectionPath(name string) string {
	file := sanitizeFileName(name) + ".nmconnection"
	return filepath.Join(b.nmDir, file)
}

func sanitizeFileName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "wg-feed"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func splitIPs(addresses []string) (ipv4 []string, ipv6 []string) {
	for _, a := range addresses {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if strings.Contains(a, ":") {
			ipv6 = append(ipv6, a)
		} else {
			ipv4 = append(ipv4, a)
		}
	}
	return ipv4, ipv6
}

func nmList(values []string) string {
	clean := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		clean = append(clean, v)
	}
	if len(clean) == 0 {
		return ""
	}
	return strings.Join(clean, ";") + ";"
}

func commaList(values []string) string {
	clean := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		clean = append(clean, v)
	}
	if len(clean) == 0 {
		return ""
	}
	return strings.Join(clean, ",")
}
