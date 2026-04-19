package windows

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/execx"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/model"
)

type runnerCall struct {
	name string
	args []string
}

type fakeRunner struct {
	calls []runnerCall
	runFn func(name string, args ...string) (execx.Result, error)
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (execx.Result, error) {
	f.calls = append(f.calls, runnerCall{name: name, args: append([]string(nil), args...)})
	if f.runFn != nil {
		return f.runFn(name, args...)
	}
	return execx.Result{}, nil
}

func newTestBackend(r *fakeRunner) *Backend {
	return New(r, slog.New(slog.NewTextHandler(io.Discard, nil)))
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

func TestApply_Disabled_UninstallsOnlyAndPersistsServiceName(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{}
	b := newTestBackend(r)

	tunnel := enabledTunnel("office")
	tunnel.EffectiveEnabled = false

	nextState, err := b.Apply(context.Background(), tunnel, state.TunnelState{})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if len(r.calls) != 2 {
		t.Fatalf("expected 2 commands (dumptunnels + uninstall), got %d", len(r.calls))
	}
	if r.calls[0].name != "wireguard.exe" || len(r.calls[0].args) != 1 || r.calls[0].args[0] != "/dumptunnels" {
		t.Fatalf("unexpected first command: %#v", r.calls[0])
	}
	if r.calls[1].name != "wireguard.exe" || len(r.calls[1].args) != 2 || r.calls[1].args[0] != "/uninstalltunnelservice" || strings.TrimSpace(r.calls[1].args[1]) == "" {
		t.Fatalf("unexpected second command: %#v", r.calls[1])
	}

	var td tunnelData
	if err := json.Unmarshal(nextState.Data, &td); err != nil {
		t.Fatalf("unmarshal state data: %v", err)
	}
	if strings.TrimSpace(td.ServiceName) == "" {
		t.Fatalf("expected persisted service name")
	}
}

func TestApply_Enabled_InstallsTunnelService(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{}
	b := newTestBackend(r)

	nextState, err := b.Apply(context.Background(), enabledTunnel("branch"), state.TunnelState{})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if len(r.calls) != 3 {
		t.Fatalf("expected 3 commands (dumptunnels + uninstall + install), got %d", len(r.calls))
	}
	install := r.calls[2]
	if install.name != "wireguard.exe" || len(install.args) != 2 || install.args[0] != "/installtunnelservice" {
		t.Fatalf("unexpected install command: %#v", install)
	}
	if !strings.HasSuffix(strings.ToLower(install.args[1]), ".conf") {
		t.Fatalf("expected install config path to end with .conf, got %q", install.args[1])
	}

	var td tunnelData
	if err := json.Unmarshal(nextState.Data, &td); err != nil {
		t.Fatalf("unmarshal state data: %v", err)
	}
	if strings.TrimSpace(td.ServiceName) == "" {
		t.Fatalf("expected persisted service name")
	}
}

func TestApply_UsesPersistedServiceNameWithoutLookup(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{}
	b := newTestBackend(r)

	data, err := json.Marshal(tunnelData{ServiceName: "persisted"})
	if err != nil {
		t.Fatalf("marshal state data: %v", err)
	}

	_, err = b.Apply(context.Background(), enabledTunnel("ignored"), state.TunnelState{Data: data})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if len(r.calls) != 2 {
		t.Fatalf("expected 2 commands (uninstall + install), got %d", len(r.calls))
	}
	if r.calls[0].args[0] != "/uninstalltunnelservice" || r.calls[0].args[1] != "persisted" {
		t.Fatalf("expected uninstall to use persisted name, got %#v", r.calls[0])
	}
}

func TestRemove_EmptyState_Noop(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{}
	b := newTestBackend(r)

	if err := b.Remove(context.Background(), state.TunnelState{}); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("expected no commands, got %d", len(r.calls))
	}
}

func TestRemove_UninstallsPersistedService(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{}
	b := newTestBackend(r)

	data, err := json.Marshal(tunnelData{ServiceName: "persisted"})
	if err != nil {
		t.Fatalf("marshal state data: %v", err)
	}

	if err := b.Remove(context.Background(), state.TunnelState{Data: data}); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("expected one uninstall command, got %d", len(r.calls))
	}
	if r.calls[0].name != "wireguard.exe" || len(r.calls[0].args) != 2 || r.calls[0].args[0] != "/uninstalltunnelservice" || r.calls[0].args[1] != "persisted" {
		t.Fatalf("unexpected command: %#v", r.calls[0])
	}
}

func TestServiceNameOccupied_MatchesCaseInsensitive(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{runFn: func(name string, args ...string) (execx.Result, error) {
		if name == "wireguard.exe" && len(args) == 1 && args[0] == "/dumptunnels" {
			return execx.Result{Stdout: "Office\n"}, nil
		}
		return execx.Result{}, nil
	}}
	b := newTestBackend(r)

	occupied, err := b.serviceNameOccupied(context.Background(), "office")
	if err != nil {
		t.Fatalf("serviceNameOccupied error: %v", err)
	}
	if !occupied {
		t.Fatalf("expected service name to be occupied")
	}
}

func TestServiceNameOccupied_DumpFailureReturnsError(t *testing.T) {
	t.Parallel()

	r := &fakeRunner{runFn: func(name string, args ...string) (execx.Result, error) {
		return execx.Result{}, errors.New("boom")
	}}
	b := newTestBackend(r)

	occupied, err := b.serviceNameOccupied(context.Background(), "office")
	if err == nil {
		t.Fatalf("expected error")
	}
	if occupied {
		t.Fatalf("expected service name to be available")
	}
}
