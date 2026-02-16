package wgquick

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/execx"
	clientwgquick "github.com/exeteres/wg-feed/internal/client/wgquick"
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
	oldAllowed, err := currentAllowedIPPrefixes(ctx, b.runner, iface)
	if err != nil {
		b.logf("wg showconf failed iface=%q err=%v", iface, err)
		oldAllowed = nil
	}

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

	newAllowed, err := allowedIPPrefixesFromConfig(stripped)
	if err != nil {
		b.logf("parse stripped config failed iface=%q err=%v", iface, err)
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
		if err := reconcileAllowedIPRoutes(ctx, b, iface, oldAllowed, newAllowed); err != nil {
			b.logf("route reconciliation failed after syncconf iface=%q err=%v", iface, err)
			return false
		}
		return true
	} else {
		b.logf("wg syncconf failed iface=%q err=%v", iface, err)
	}
	if _, err := b.runner.Run(ctx, "wg", "setconf", iface, tmp); err == nil {
		if err := reconcileAllowedIPRoutes(ctx, b, iface, oldAllowed, newAllowed); err != nil {
			b.logf("route reconciliation failed after setconf iface=%q err=%v", iface, err)
			return false
		}
		return true
	} else {
		b.logf("wg setconf failed iface=%q err=%v", iface, err)
	}
	return false
}

func currentAllowedIPPrefixes(ctx context.Context, runner Runner, iface string) (map[netip.Prefix]struct{}, error) {
	res, err := runner.Run(ctx, "wg", "showconf", iface)
	if err != nil {
		return nil, err
	}
	return allowedIPPrefixesFromConfig(res.Stdout)
}

func allowedIPPrefixesFromConfig(conf string) (map[netip.Prefix]struct{}, error) {
	cfg, err := clientwgquick.Parse([]byte(conf))
	if err != nil {
		return nil, err
	}
	out := map[netip.Prefix]struct{}{}
	for _, p := range cfg.Peers {
		for _, raw := range p.AllowedIPs {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			pref, err := netip.ParsePrefix(raw)
			if err != nil {
				// Ignore unparseable entries (best-effort). Validate layer should generally prevent this.
				continue
			}
			out[pref] = struct{}{}
		}
	}
	return out, nil
}

func reconcileAllowedIPRoutes(ctx context.Context, b *Backend, iface string, oldAllowed, newAllowed map[netip.Prefix]struct{}) error {
	// If we couldn't determine old state, only ensure new routes exist.
	if oldAllowed == nil {
		for p := range newAllowed {
			if err := routeReplace(ctx, b.runner, iface, p); err != nil {
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

	// Add first, then delete.
	for _, p := range toAdd {
		if err := routeReplace(ctx, b.runner, iface, p); err != nil {
			return err
		}
	}
	for _, p := range toDel {
		if err := routeDelete(ctx, b.runner, iface, p); err != nil {
			// Best-effort: deletions may race with external tools.
			b.logf("ip route del failed iface=%q prefix=%q err=%v", iface, p.String(), err)
		}
	}
	return nil
}

func routeReplace(ctx context.Context, runner Runner, iface string, p netip.Prefix) error {
	args := []string{"route", "replace", p.String(), "dev", iface}
	if p.Addr().Is6() {
		_, err := runner.Run(ctx, "ip", append([]string{"-6"}, args...)...)
		return err
	}
	_, err := runner.Run(ctx, "ip", append([]string{"-4"}, args...)...)
	return err
}

func routeDelete(ctx context.Context, runner Runner, iface string, p netip.Prefix) error {
	args := []string{"route", "del", p.String(), "dev", iface}
	if p.Addr().Is6() {
		_, err := runner.Run(ctx, "ip", append([]string{"-6"}, args...)...)
		return err
	}
	_, err := runner.Run(ctx, "ip", append([]string{"-4"}, args...)...)
	return err
}

func (b *Backend) logf(format string, args ...any) {
	if b.logger == nil {
		return
	}
	b.logger.Printf(format, args...)
}
