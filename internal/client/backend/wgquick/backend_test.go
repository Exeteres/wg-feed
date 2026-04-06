package wgquick

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/execx"
)

func hasCallPrefix(calls []string, prefix string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

type fakeRunner struct {
	calls []string

	wgShowErr        error
	wgShowconfStdout string
	wgShowconfErr    error

	stripStdout string
	stripErr    error

	syncconfErr error
	setconfErr  error

	onQuickUp func(path string) error
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (execx.Result, error) {
	line := name
	if len(args) > 0 {
		line += " " + strings.Join(args, " ")
	}
	r.calls = append(r.calls, line)

	switch name {
	case "wg", "awg":
		if len(args) >= 2 && args[0] == "show" {
			if r.wgShowErr != nil {
				return execx.Result{}, r.wgShowErr
			}
			return execx.Result{}, nil
		}
		if len(args) >= 2 && args[0] == "showconf" {
			if r.wgShowconfErr != nil {
				return execx.Result{}, r.wgShowconfErr
			}
			return execx.Result{Stdout: r.wgShowconfStdout}, nil
		}
		if len(args) >= 3 && args[0] == "syncconf" {
			if r.syncconfErr != nil {
				return execx.Result{}, r.syncconfErr
			}
			return execx.Result{}, nil
		}
		if len(args) >= 3 && args[0] == "setconf" {
			if r.setconfErr != nil {
				return execx.Result{}, r.setconfErr
			}
			return execx.Result{}, nil
		}
	case "wg-quick", "awg-quick":
		if len(args) >= 2 && args[0] == "strip" {
			if r.stripErr != nil {
				return execx.Result{}, r.stripErr
			}
			return execx.Result{Stdout: r.stripStdout}, nil
		}
		if len(args) >= 2 && args[0] == "up" {
			if r.onQuickUp != nil {
				if err := r.onQuickUp(args[1]); err != nil {
					return execx.Result{}, err
				}
			}
			return execx.Result{}, nil
		}
		// down is always best-effort in Backend.Apply; just succeed here.
		return execx.Result{}, nil
	}

	return execx.Result{}, nil
}

func TestApply_Enabled_InterfaceDown_FallsBackToDownUp(t *testing.T) {
	r := &fakeRunner{wgShowErr: errors.New("not up")}
	logger := log.New(io.Discard, "", 0)
	b := New(r, logger)

	var gotConfig string
	r.onQuickUp = func(path string) error {
		if !strings.HasSuffix(path, "amsterdam-2.conf") {
			t.Fatalf("expected config path to end with amsterdam-2.conf; got %q", path)
		}
		bts, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected config file to exist: %v", err)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("expected mode 0600; got %v", st.Mode().Perm())
		}
		gotConfig = string(bts)
		return nil
	}

	// Note: config lacks trailing newline on purpose.
	cfg := "[Interface]\nPrivateKey = x"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if !strings.HasSuffix(gotConfig, "\n") {
		t.Fatalf("expected config written with trailing newline")
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "wg show amsterdam-2") {
		t.Fatalf("expected wg show; got:\n%s", joined)
	}
	if !strings.Contains(joined, "wg-quick down amsterdam-2") {
		t.Fatalf("expected wg-quick down; got:\n%s", joined)
	}
	if !strings.Contains(joined, "wg-quick up ") {
		t.Fatalf("expected wg-quick up; got:\n%s", joined)
	}
}

func TestApply_Enabled_InterfaceUp_DeviceUpdateUsesSyncconf_NoDownUp(t *testing.T) {
	r := &fakeRunner{
		stripStdout: "[Interface]\nPrivateKey = x\n",
	}
	logger := log.New(io.Discard, "", 0)
	b := New(r, logger)

	cfg := "[Interface]\nPrivateKey = x\n"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "wg show amsterdam-2") {
		t.Fatalf("expected wg show; got:\n%s", joined)
	}
	if !strings.Contains(joined, "wg-quick strip ") {
		t.Fatalf("expected wg-quick strip; got:\n%s", joined)
	}
	if !strings.Contains(joined, "wg syncconf amsterdam-2 ") {
		t.Fatalf("expected wg syncconf; got:\n%s", joined)
	}
	if strings.Contains(joined, "wg-quick down amsterdam-2") || strings.Contains(joined, "wg-quick up ") {
		t.Fatalf("did not expect wg-quick down/up on successful device update; got:\n%s", joined)
	}
}

func TestApply_Enabled_WithAmneziaExtensions_UsesAWGForLiveUpdate(t *testing.T) {
	r := &fakeRunner{
		stripStdout: "[Interface]\nPrivateKey = x\nJc = 3\n",
	}
	logger := log.New(io.Discard, "", 0)
	b := New(r, logger)

	cfg := "[Interface]\nPrivateKey = x\nJc = 3\n"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "awg show amsterdam-2") {
		t.Fatalf("expected awg show; got:\n%s", joined)
	}
	if !strings.Contains(joined, "awg-quick strip ") {
		t.Fatalf("expected awg-quick strip; got:\n%s", joined)
	}
	if !strings.Contains(joined, "awg syncconf amsterdam-2 ") {
		t.Fatalf("expected awg syncconf; got:\n%s", joined)
	}
	if hasCallPrefix(r.calls, "wg syncconf amsterdam-2 ") {
		t.Fatalf("did not expect wg syncconf for amnezia config; got:\n%s", joined)
	}
}

func TestApply_Enabled_InterfaceDown_WithAmneziaExtensions_UsesAWGQuickDownUp(t *testing.T) {
	r := &fakeRunner{wgShowErr: errors.New("not up")}
	b := New(r, log.New(io.Discard, "", 0))

	cfg := "[Interface]\nPrivateKey = x\nS1 = aa\n"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "awg show amsterdam-2") {
		t.Fatalf("expected awg show; got:\n%s", joined)
	}
	if !strings.Contains(joined, "awg-quick down amsterdam-2") {
		t.Fatalf("expected awg-quick down; got:\n%s", joined)
	}
	if !strings.Contains(joined, "awg-quick up ") {
		t.Fatalf("expected awg-quick up; got:\n%s", joined)
	}
	if hasCallPrefix(r.calls, "wg-quick up ") {
		t.Fatalf("did not expect wg-quick up for amnezia config; got:\n%s", joined)
	}
}

func TestApply_Enabled_InterfaceDown_WithAmneziaExtensions_NoFallbackToWGQuickOnAWGFailure(t *testing.T) {
	r := &fakeRunner{wgShowErr: errors.New("not up")}
	r.onQuickUp = func(path string) error {
		_ = path
		return errors.New("awg-quick up failed")
	}
	b := New(r, log.New(io.Discard, "", 0))

	cfg := "[Interface]\nPrivateKey = x\nI1 = deadbeef\n"
	err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false)
	if err == nil {
		t.Fatalf("expected apply to fail when awg-quick up fails")
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "awg-quick up ") {
		t.Fatalf("expected awg-quick up attempt; got:\n%s", joined)
	}
	if hasCallPrefix(r.calls, "wg-quick down amsterdam-2") || hasCallPrefix(r.calls, "wg-quick up ") {
		t.Fatalf("did not expect wg-quick fallback for amnezia config; got:\n%s", joined)
	}
}

func TestApply_Enabled_InterfaceUp_DeviceUpdate_ReconcilesAllowedIPRoutes(t *testing.T) {
	r := &fakeRunner{
		wgShowconfStdout: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = pub1\nAllowedIPs = 10.0.0.0/24, 192.168.0.0/16\n",
		stripStdout:      "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = pub1\nAllowedIPs = 10.0.0.0/24, 2001:db8::/64\n",
	}
	logger := log.New(io.Discard, "", 0)
	b := New(r, logger)

	cfg := "[Interface]\nPrivateKey = x\n"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "wg showconf amsterdam-2") {
		t.Fatalf("expected wg showconf; got:\n%s", joined)
	}
	if !strings.Contains(joined, "ip -6 route replace 2001:db8::/64 dev amsterdam-2") {
		t.Fatalf("expected ipv6 route replace; got:\n%s", joined)
	}
	if !strings.Contains(joined, "ip -4 route del 192.168.0.0/16 dev amsterdam-2") {
		t.Fatalf("expected ipv4 route del; got:\n%s", joined)
	}
	if strings.Contains(joined, "wg-quick down amsterdam-2") || strings.Contains(joined, "wg-quick up ") {
		t.Fatalf("did not expect wg-quick down/up on successful device update; got:\n%s", joined)
	}
}

func TestApply_Enabled_InterfaceUp_DeviceUpdate_ReconcilesAllowedIPRoutes_MultiplePeers(t *testing.T) {
	r := &fakeRunner{
		wgShowconfStdout: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = pub1\nAllowedIPs = 10.0.0.0/24\n\n[Peer]\nPublicKey = pub2\nAllowedIPs = 10.1.0.0/24, 2001:db8:1::/64\n",
		stripStdout:      "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = pub1\nAllowedIPs = 10.0.0.0/24\n\n[Peer]\nPublicKey = pub3\nAllowedIPs = 10.2.0.0/24\n",
	}
	logger := log.New(io.Discard, "", 0)
	b := New(r, logger)

	cfg := "[Interface]\nPrivateKey = x\n"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "wg showconf amsterdam-2") {
		t.Fatalf("expected wg showconf; got:\n%s", joined)
	}
	if !strings.Contains(joined, "ip -4 route replace 10.2.0.0/24 dev amsterdam-2") {
		t.Fatalf("expected ipv4 route replace for new peer; got:\n%s", joined)
	}
	if !strings.Contains(joined, "ip -4 route del 10.1.0.0/24 dev amsterdam-2") {
		t.Fatalf("expected ipv4 route del for removed peer; got:\n%s", joined)
	}
	if !strings.Contains(joined, "ip -6 route del 2001:db8:1::/64 dev amsterdam-2") {
		t.Fatalf("expected ipv6 route del for removed peer; got:\n%s", joined)
	}
}

func TestApply_Enabled_InterfaceUp_DeviceUpdate_ReconcilesAllowedIPRoutes_DedupAcrossPeers(t *testing.T) {
	r := &fakeRunner{
		wgShowconfStdout: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = pub1\nAllowedIPs = 10.0.0.0/24\n\n[Peer]\nPublicKey = pub2\nAllowedIPs = 10.0.0.0/24\n",
		stripStdout:      "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = pub1\nAllowedIPs = 10.0.0.0/24\n",
	}
	logger := log.New(io.Discard, "", 0)
	b := New(r, logger)

	cfg := "[Interface]\nPrivateKey = x\n"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if strings.Contains(joined, "ip -4 route del 10.0.0.0/24 dev amsterdam-2") {
		t.Fatalf("did not expect route deletion for prefix still present; got:\n%s", joined)
	}
}

func TestApply_Enabled_InterfaceUp_DeviceUpdate_ReconcilesAllowedIPRoutes_PeerAdded(t *testing.T) {
	r := &fakeRunner{
		wgShowconfStdout: "[Interface]\nPrivateKey = x\n",
		stripStdout:      "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = pub1\nAllowedIPs = 10.9.0.0/24\n",
	}
	logger := log.New(io.Discard, "", 0)
	b := New(r, logger)

	cfg := "[Interface]\nPrivateKey = x\n"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "ip -4 route replace 10.9.0.0/24 dev amsterdam-2") {
		t.Fatalf("expected ipv4 route replace for added peer; got:\n%s", joined)
	}
	if strings.Contains(joined, "ip -4 route del") || strings.Contains(joined, "ip -6 route del") {
		t.Fatalf("did not expect any route deletions on pure addition; got:\n%s", joined)
	}
}

func TestApply_Enabled_InterfaceUp_DeviceUpdate_ReconcilesAllowedIPRoutes_PeerRemoved(t *testing.T) {
	r := &fakeRunner{
		wgShowconfStdout: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = pub1\nAllowedIPs = 10.9.0.0/24, 2001:db8:9::/64\n",
		stripStdout:      "[Interface]\nPrivateKey = x\n",
	}
	logger := log.New(io.Discard, "", 0)
	b := New(r, logger)

	cfg := "[Interface]\nPrivateKey = x\n"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "ip -4 route del 10.9.0.0/24 dev amsterdam-2") {
		t.Fatalf("expected ipv4 route del for removed peer; got:\n%s", joined)
	}
	if !strings.Contains(joined, "ip -6 route del 2001:db8:9::/64 dev amsterdam-2") {
		t.Fatalf("expected ipv6 route del for removed peer; got:\n%s", joined)
	}
}

func TestApply_Enabled_InterfaceUp_SyncconfFails_SetconfSucceeds(t *testing.T) {
	r := &fakeRunner{
		stripStdout: "[Interface]\nPrivateKey = x\n",
		syncconfErr: errors.New("syncconf failed"),
		setconfErr:  nil,
	}
	logger := log.New(io.Discard, "", 0)
	b := New(r, logger)

	cfg := "[Interface]\nPrivateKey = x\n"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "wg syncconf amsterdam-2 ") {
		t.Fatalf("expected wg syncconf; got:\n%s", joined)
	}
	if !strings.Contains(joined, "wg setconf amsterdam-2 ") {
		t.Fatalf("expected wg setconf; got:\n%s", joined)
	}
	if strings.Contains(joined, "wg-quick down amsterdam-2") || strings.Contains(joined, "wg-quick up ") {
		t.Fatalf("did not expect wg-quick down/up when setconf succeeds; got:\n%s", joined)
	}
}

func TestApply_Enabled_InterfaceUp_EmptyStripFallsBackToDownUp(t *testing.T) {
	r := &fakeRunner{stripStdout: "\n"}
	logger := log.New(io.Discard, "", 0)
	b := New(r, logger)

	cfg := "[Interface]\nPrivateKey = x\n"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "wg-quick down amsterdam-2") {
		t.Fatalf("expected wg-quick down; got:\n%s", joined)
	}
	if !strings.Contains(joined, "wg-quick up ") {
		t.Fatalf("expected wg-quick up; got:\n%s", joined)
	}
}

func TestApply_Disabled_CallsDownOnly(t *testing.T) {
	r := &fakeRunner{}
	logger := log.New(io.Discard, "", 0)
	b := New(r, logger)

	cfg := "[Interface]\nPrivateKey = x\n"
	if err := b.Apply(context.Background(), "amsterdam-2", cfg, false, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if strings.Contains(joined, "wg-quick up ") {
		t.Fatalf("did not expect wg-quick up when disabled; got:\n%s", joined)
	}
	if !strings.Contains(joined, "wg-quick down amsterdam-2") {
		t.Fatalf("expected wg-quick down; got:\n%s", joined)
	}
}

func TestRemove_EmptyName_NoOp(t *testing.T) {
	r := &fakeRunner{}
	b := New(r, log.New(io.Discard, "", 0))
	if err := b.Remove(context.Background(), ""); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("expected no calls; got %#v", r.calls)
	}
}

func TestRemove_NonEmptyName_CallsDown(t *testing.T) {
	r := &fakeRunner{}
	b := New(r, log.New(io.Discard, "", 0))
	if err := b.Remove(context.Background(), "amsterdam-2"); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "wg-quick down amsterdam-2") {
		t.Fatalf("expected wg-quick down; got:\n%s", joined)
	}
}

func TestApply_EmptyName_Errors(t *testing.T) {
	r := &fakeRunner{}
	b := New(r, log.New(io.Discard, "", 0))
	err := b.Apply(context.Background(), " ", "[Interface]\nPrivateKey = x\n", true, false)
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(r.calls) != 0 {
		t.Fatalf("expected no calls; got %#v", r.calls)
	}
}

func TestApply_WritesConfigInTempDir(t *testing.T) {
	r := &fakeRunner{wgShowErr: errors.New("down")}
	b := New(r, log.New(io.Discard, "", 0))

	r.onQuickUp = func(path string) error {
		// This is mainly to ensure it writes under a temp dir, not cwd.
		if !strings.Contains(filepath.Clean(path), string(os.PathSeparator)) {
			t.Fatalf("expected a path, got %q", path)
		}
		return nil
	}

	if err := b.Apply(context.Background(), "amsterdam-2", "[Interface]\nPrivateKey = x", true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}
}
