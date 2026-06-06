package windowsmanager

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/model"
)

func newTestBackend() *Backend {
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func enabledTunnel(name string) shared.ResolvedTunnel {
	return shared.ResolvedTunnel{
		Tunnel: model.Tunnel{
			ID:            "t1",
			Name:          name,
			Enabled:       true,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 10.0.0.0/24\n",
		},
		EffectiveEnabled: true,
	}
}

func enabledAmneziaTunnel(name string) shared.ResolvedTunnel {
	tunnel := enabledTunnel(name)
	tunnel.WGQuickConfig = "[Interface]\nPrivateKey = x\nJc = 4\n\n[Peer]\nPublicKey = y\nAllowedIPs = 10.0.0.0/24\n"
	return tunnel
}

func TestApply_Enabled_StagesWireGuardConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramFiles", tmp)

	b := newTestBackend()
	next, err := b.Apply(context.Background(), enabledTunnel("office"), state.TunnelState{})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	path := managerConfPath("office", false)
	if !strings.HasPrefix(path, filepath.Join(tmp, "WireGuard", "Data", "Configurations")+string(filepath.Separator)) {
		t.Fatalf("unexpected config path: %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected staged config, stat err=%v", err)
	}
	var td tunnelData
	if err := json.Unmarshal(next.Data, &td); err != nil {
		t.Fatalf("unmarshal state data: %v", err)
	}
	if td.ConfigName != "office" || td.Amnezia {
		t.Fatalf("unexpected state data: %+v", td)
	}
}

func TestApply_Enabled_StagesAmneziaConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramFiles", tmp)

	b := newTestBackend()
	next, err := b.Apply(context.Background(), enabledAmneziaTunnel("branch"), state.TunnelState{})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	path := managerConfPath("branch", true)
	if !strings.HasPrefix(path, filepath.Join(tmp, "AmneziaWG", "Data", "Configurations")+string(filepath.Separator)) {
		t.Fatalf("unexpected config path: %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected staged config, stat err=%v", err)
	}
	var td tunnelData
	if err := json.Unmarshal(next.Data, &td); err != nil {
		t.Fatalf("unmarshal state data: %v", err)
	}
	if td.ConfigName != "branch" || !td.Amnezia {
		t.Fatalf("unexpected state data: %+v", td)
	}
}

func TestApply_Disabled_RemovesDPAPIFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramFiles", tmp)

	name := "office"
	dpapiPath := managerDPAPIPath(name, false)
	if err := os.MkdirAll(filepath.Dir(dpapiPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dpapiPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := json.Marshal(tunnelData{ConfigName: name})
	if err != nil {
		t.Fatalf("marshal state data: %v", err)
	}

	tunnel := enabledTunnel("ignored")
	tunnel.EffectiveEnabled = false

	b := newTestBackend()
	if _, err := b.Apply(context.Background(), tunnel, state.TunnelState{Data: data}); err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if _, err := os.Stat(dpapiPath); !os.IsNotExist(err) {
		t.Fatalf("expected dpapi file removed, stat err=%v", err)
	}
}

func TestApply_Enabled_RemovesExistingDPAPIBeforeStaging(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramFiles", tmp)

	name := "office"
	dpapiPath := managerDPAPIPath(name, false)
	if err := os.MkdirAll(filepath.Dir(dpapiPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dpapiPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := json.Marshal(tunnelData{ConfigName: name})
	if err != nil {
		t.Fatalf("marshal state data: %v", err)
	}

	b := newTestBackend()
	if _, err := b.Apply(context.Background(), enabledTunnel("ignored"), state.TunnelState{Data: data}); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if _, err := os.Stat(dpapiPath); !os.IsNotExist(err) {
		t.Fatalf("expected dpapi file removed, stat err=%v", err)
	}
	if _, err := os.Stat(managerConfPath(name, false)); err != nil {
		t.Fatalf("expected staged conf, stat err=%v", err)
	}
}

func TestRemove_RemovesDPAPIFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ProgramFiles", tmp)

	name := "office"
	dpapiPath := managerDPAPIPath(name, true)
	if err := os.MkdirAll(filepath.Dir(dpapiPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dpapiPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, err := json.Marshal(tunnelData{ConfigName: name, Amnezia: true})
	if err != nil {
		t.Fatalf("marshal state data: %v", err)
	}

	b := newTestBackend()
	if err := b.Remove(context.Background(), state.TunnelState{Data: data}); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if _, err := os.Stat(dpapiPath); !os.IsNotExist(err) {
		t.Fatalf("expected dpapi file removed, stat err=%v", err)
	}
}
