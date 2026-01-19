package networkmanager

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/backend/networkmanager/nmconfig"
	"github.com/exeteres/wg-feed/internal/client/execx"
	"github.com/exeteres/wg-feed/internal/client/wgquick"
	"github.com/google/uuid"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) (execx.Result, error)
}

type Backend struct {
	runner   Runner
	logger   *log.Logger
	nmDir    string
	read     func(string) ([]byte, error)
	write    func(string, []byte, os.FileMode) error
	mkdirAll func(string, os.FileMode) error
	remove   func(string) error
	uuidGen  func() string
}

func New(runner Runner, logger *log.Logger) *Backend {
	return &Backend{
		runner:   runner,
		logger:   logger,
		nmDir:    "/etc/NetworkManager/system-connections",
		read:     os.ReadFile,
		write:    os.WriteFile,
		mkdirAll: os.MkdirAll,
		remove:   os.Remove,
		uuidGen:  uuid.NewString,
	}
}

func (b *Backend) Apply(ctx context.Context, name string, wgQuickConfig string, enabled bool) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("networkmanager backend requires a non-empty connection name")
	}

	parsed, err := wgquick.Parse([]byte(wgQuickConfig))
	if err != nil {
		return fmt.Errorf("parse wg-quick config: %w", err)
	}
	if strings.TrimSpace(parsed.Interface.PrivateKey) == "" {
		return errors.New("wg-quick config missing [Interface] PrivateKey")
	}
	if len(parsed.Peers) == 0 {
		return errors.New("wg-quick config missing at least one [Peer]")
	}

	nmPath := b.nmConnectionPath(name)
	var existing []byte
	if data, err := b.read(nmPath); err == nil {
		existing = data
	}

	out, err := buildNMConnection(existing, name, parsed, b.uuidGen)
	if err != nil {
		return err
	}

	if err := b.mkdirAll(filepath.Dir(nmPath), 0o755); err != nil {
		return fmt.Errorf("mkdir nm dir: %w", err)
	}
	if err := b.write(nmPath, out, 0o600); err != nil {
		return fmt.Errorf("write nmconnection: %w", err)
	}

	_, _ = b.runner.Run(ctx, "nmcli", "connection", "reload")
	if enabled {
		_, err = b.runner.Run(ctx, "nmcli", "connection", "up", "id", name)
		return err
	}
	_, err = b.runner.Run(ctx, "nmcli", "connection", "down", "id", name)
	return err
}

func (b *Backend) Remove(ctx context.Context, name string) error {
	_, _ = b.runner.Run(ctx, "nmcli", "connection", "down", "id", name)
	_, _ = b.runner.Run(ctx, "nmcli", "connection", "delete", "id", name)
	_ = b.remove(b.nmConnectionPath(name))
	return nil
}

func buildNMConnection(existing []byte, name string, parsed wgquick.Config, uuidGen func() string) ([]byte, error) {
	kf := nmconfig.NewEmpty()
	if len(existing) > 0 {
		parsedKF, err := nmconfig.Parse(existing)
		if err != nil {
			return nil, fmt.Errorf("parse nmconnection: %w", err)
		}
		kf = parsedKF
	}

	uuidVal, ok := kf.Get("connection", "uuid")
	if !ok || strings.TrimSpace(uuidVal) == "" {
		uuidVal = uuidGen()
	}

	// [connection]
	kf.Set("connection", "id", name)
	kf.Set("connection", "uuid", uuidVal)
	kf.Set("connection", "type", "wireguard")
	kf.Set("connection", "interface-name", name)

	// [wireguard]
	if parsed.Interface.MTU != nil {
		kf.Set("wireguard", "mtu", fmt.Sprintf("%d", *parsed.Interface.MTU))
	}
	kf.Set("wireguard", "private-key", parsed.Interface.PrivateKey)

	// Peer sections: rebuild those only.
	kf.RemoveSectionsWithPrefix("wireguard-peer.")
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

	ipv4Addrs, ipv6Addrs := splitIPs(parsed.Interface.Addresses)
	if len(ipv4Addrs) > 0 {
		kf.Set("ipv4", "method", "manual")
		kf.Set("ipv4", "address1", ipv4Addrs[0])
	} else {
		kf.Set("ipv4", "method", "disabled")
	}
	if len(parsed.Interface.DNS) > 0 {
		kf.Set("ipv4", "dns", nmList(parsed.Interface.DNS))
		kf.Set("ipv4", "dns-search", "~;")
	}

	if len(ipv6Addrs) > 0 {
		kf.Set("ipv6", "method", "manual")
		kf.Set("ipv6", "address1", ipv6Addrs[0])
	} else {
		kf.Set("ipv6", "method", "disabled")
		kf.Set("ipv6", "addr-gen-mode", "default")
	}

	return kf.Bytes(), nil
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
