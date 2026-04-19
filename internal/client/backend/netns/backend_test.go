package netns

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/execx"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/model"
)

type fakeRunner struct {
	calls      []string
	netnsList  string
	linkByName map[string]bool
	linkByNS   map[string]map[string]bool
	errFn      func(call string) error
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (execx.Result, error) {
	call := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, call)
	if r.errFn != nil {
		if err := r.errFn(call); err != nil {
			return execx.Result{}, err
		}
	}

	if call == "ip netns list" {
		return execx.Result{Stdout: r.netnsList}, nil
	}
	if strings.HasPrefix(call, "ip -n ") && strings.Contains(call, " -o link show dev ") {
		nsAndRest := strings.TrimPrefix(call, "ip -n ")
		parts := strings.SplitN(nsAndRest, " -o link show dev ", 2)
		if len(parts) == 2 {
			ns := strings.TrimSpace(parts[0])
			dev := strings.TrimSpace(parts[1])
			if r.linkByNS != nil && r.linkByNS[ns] != nil && r.linkByNS[ns][dev] {
				return execx.Result{Stdout: "1: " + dev + ": <BROADCAST>"}, nil
			}
		}
		return execx.Result{}, errors.New("not found")
	}
	if strings.HasPrefix(call, "ip -o link show dev ") {
		dev := strings.TrimPrefix(call, "ip -o link show dev ")
		if r.linkByName != nil && r.linkByName[dev] {
			return execx.Result{Stdout: "1: " + dev + ": <BROADCAST>"}, nil
		}
		return execx.Result{}, errors.New("not found")
	}
	if strings.Contains(call, " wg-quick strip ") || strings.HasPrefix(call, "wg-quick strip ") {
		return execx.Result{Stdout: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 10.0.0.0/24\n"}, nil
	}
	if strings.Contains(call, " awg-quick strip ") || strings.HasPrefix(call, "awg-quick strip ") {
		return execx.Result{Stdout: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 10.0.0.0/24\n"}, nil
	}
	return execx.Result{}, nil
}

func testTunnel(name, cfg string) shared.ResolvedTunnel {
	return shared.ResolvedTunnel{Tunnel: model.Tunnel{Name: name, WGQuickConfig: cfg, Enabled: true}, EffectiveEnabled: true}
}

func testTunnelDisabled(name, cfg string) shared.ResolvedTunnel {
	return shared.ResolvedTunnel{Tunnel: model.Tunnel{Name: name, WGQuickConfig: cfg, Enabled: false}, EffectiveEnabled: false}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type tunnelDataForTest struct {
	DeviceName string `json:"device_name"`
	Namespace  string `json:"namespace"`
}

func TestApply_Enabled_MovesExistingInterfaceFromInitNamespace(t *testing.T) {
	r := &fakeRunner{netnsList: "", linkByName: map[string]bool{"paris": true}}
	b := New(r, testLogger())

	tunnel := testTunnel("paris", "[Interface]\nPrivateKey = x\nAddress = 10.20.0.2/32\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n")
	data, _ := json.Marshal(tunnelDataForTest{DeviceName: "paris", Namespace: "paris"})
	st, err := b.Apply(context.Background(), tunnel, state.TunnelState{Data: data})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	var td tunnelDataForTest
	if err := json.Unmarshal(st.Data, &td); err != nil {
		t.Fatalf("unmarshal state data: %v", err)
	}
	if td.DeviceName != "paris" {
		t.Fatalf("device name mismatch: %q", td.DeviceName)
	}
	if td.Namespace != "paris" {
		t.Fatalf("namespace mismatch: %q", td.Namespace)
	}

	joined := strings.Join(r.calls, "\n")
	for _, expected := range []string{
		"ip netns add paris",
		"ip link set dev paris netns paris",
		"ip netns exec paris wg setconf paris",
		"ip -n paris address replace 10.20.0.2/32 dev paris",
		"ip -n paris link set dev paris up",
		"ip -4 -n paris route replace 0.0.0.0/0 dev paris",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected call containing %q; got:\n%s", expected, joined)
		}
	}
	if strings.Contains(joined, "ip link add dev paris") {
		t.Fatalf("did not expect interface creation; got:\n%s", joined)
	}
}

func TestApply_Enabled_ReusesPersistedNames(t *testing.T) {
	r := &fakeRunner{
		netnsList: "persisted-ns\n",
		linkByNS: map[string]map[string]bool{
			"persisted-ns": {
				"persisted-dev": true,
			},
		},
	}
	b := New(r, testLogger())

	data, _ := json.Marshal(tunnelDataForTest{DeviceName: "persisted-dev", Namespace: "persisted-ns"})
	st, err := b.Apply(context.Background(), testTunnel("hint-name", "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 10.0.0.0/24\n"), state.TunnelState{Data: data})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	var td tunnelDataForTest
	if err := json.Unmarshal(st.Data, &td); err != nil {
		t.Fatalf("unmarshal state data: %v", err)
	}
	if td.DeviceName != "persisted-dev" || td.Namespace != "persisted-ns" {
		t.Fatalf("unexpected persisted data: %+v", td)
	}

	joined := strings.Join(r.calls, "\n")
	if strings.Contains(joined, "ip netns add persisted-ns") {
		t.Fatalf("did not expect namespace add when already present; got:\n%s", joined)
	}
	if strings.Contains(joined, "ip link set dev persisted-dev netns persisted-ns") {
		t.Fatalf("did not expect move when interface already in target namespace; got:\n%s", joined)
	}
}

func TestApply_Enabled_CreatesInInitAndMovesWhenMissingInTarget(t *testing.T) {
	r := &fakeRunner{netnsList: ""}
	b := New(r, testLogger())

	st, err := b.Apply(context.Background(), testTunnel("paris", "[Interface]\nPrivateKey = x\nAddress = 10.20.0.2/32\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n"), state.TunnelState{})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if len(st.Data) == 0 {
		t.Fatalf("expected state data to be persisted")
	}

	joined := strings.Join(r.calls, "\n")
	for _, expected := range []string{
		"ip netns add paris",
		"ip link add dev paris type wireguard",
		"ip link set dev paris netns paris",
		"ip netns exec paris wg setconf paris",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected call containing %q; got:\n%s", expected, joined)
		}
	}
}

func TestApply_Disabled_Teardown(t *testing.T) {
	r := &fakeRunner{}
	b := New(r, testLogger())
	data, _ := json.Marshal(tunnelDataForTest{DeviceName: "amsterdam", Namespace: "amsterdam"})

	st, err := b.Apply(context.Background(), testTunnelDisabled("ignored", "[Interface]\nPrivateKey = x\n"), state.TunnelState{Data: data})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if len(st.Data) != 0 {
		t.Fatalf("expected disabled apply to clear state data")
	}
	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "ip -n amsterdam link del dev amsterdam") {
		t.Fatalf("expected namespace link delete; got:\n%s", joined)
	}
	if !strings.Contains(joined, "ip netns del amsterdam") {
		t.Fatalf("expected namespace delete; got:\n%s", joined)
	}
}

func TestRemove_UsesStateNames(t *testing.T) {
	r := &fakeRunner{}
	b := New(r, testLogger())
	data, _ := json.Marshal(tunnelDataForTest{DeviceName: "rome", Namespace: "rome"})
	if err := b.Remove(context.Background(), state.TunnelState{Data: data}); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "ip -n rome link del dev rome") {
		t.Fatalf("expected namespace link delete; got:\n%s", joined)
	}
	if !strings.Contains(joined, "ip netns del rome") {
		t.Fatalf("expected namespace delete; got:\n%s", joined)
	}
}

func TestApply_Disabled_WithoutState_DoesNotAllocateNames(t *testing.T) {
	r := &fakeRunner{}
	b := New(r, testLogger())

	st, err := b.Apply(context.Background(), testTunnelDisabled("hint", "[Interface]\nPrivateKey = x\n"), state.TunnelState{})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	if len(st.Data) != 0 {
		t.Fatalf("expected disabled apply to keep state data empty")
	}
	if len(r.calls) != 0 {
		t.Fatalf("expected no commands when disabled with empty state; got %#v", r.calls)
	}
}

func TestApply_Enabled_ReclaimsBaseNameWhenNamespaceExists(t *testing.T) {
	r := &fakeRunner{netnsList: "amsterdam\n"}
	b := New(r, testLogger())

	st, err := b.Apply(context.Background(), testTunnel("amsterdam", "[Interface]\nPrivateKey = x\nAddress = 10.20.0.2/32\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n"), state.TunnelState{})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	var td tunnelDataForTest
	if err := json.Unmarshal(st.Data, &td); err != nil {
		t.Fatalf("unmarshal state data: %v", err)
	}
	if td.DeviceName != "amsterdam" || td.Namespace != "amsterdam" {
		t.Fatalf("expected base name reclaim, got %+v", td)
	}
}

func TestApply_Disabled_TeardownFailurePreservesState(t *testing.T) {
	r := &fakeRunner{
		errFn: func(call string) error {
			if call == "ip netns del amsterdam" {
				return errors.New("busy")
			}
			return nil
		},
	}
	b := New(r, testLogger())
	data, _ := json.Marshal(tunnelDataForTest{DeviceName: "amsterdam", Namespace: "amsterdam"})

	st, err := b.Apply(context.Background(), testTunnelDisabled("ignored", "[Interface]\nPrivateKey = x\n"), state.TunnelState{Data: data})
	if err == nil {
		t.Fatal("expected teardown error")
	}
	if len(st.Data) == 0 {
		t.Fatalf("expected state data to remain when teardown fails")
	}
}

func TestApply_Enabled_WritesNamespaceResolvConf(t *testing.T) {
	r := &fakeRunner{netnsList: ""}
	b := New(r, testLogger())
	b.netnsEtc = t.TempDir()

	_, err := b.Apply(context.Background(), testTunnel("dnsns", "[Interface]\nPrivateKey = x\nAddress = 10.20.0.2/32\nDNS = 1.1.1.1, 8.8.8.8\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n"), state.TunnelState{})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	resolvPath := filepath.Join(b.netnsEtc, "dnsns", "resolv.conf")
	bts, err := os.ReadFile(resolvPath)
	if err != nil {
		t.Fatalf("read resolv.conf: %v", err)
	}
	got := string(bts)
	if !strings.Contains(got, "nameserver 1.1.1.1\n") {
		t.Fatalf("expected nameserver 1.1.1.1 in resolv.conf, got: %q", got)
	}
	if !strings.Contains(got, "nameserver 8.8.8.8\n") {
		t.Fatalf("expected nameserver 8.8.8.8 in resolv.conf, got: %q", got)
	}
}

func TestApply_Enabled_EmptyDNSRemovesNamespaceResolvConf(t *testing.T) {
	r := &fakeRunner{netnsList: ""}
	b := New(r, testLogger())
	b.netnsEtc = t.TempDir()

	resolvPath := filepath.Join(b.netnsEtc, "dnsns", "resolv.conf")
	if err := os.MkdirAll(filepath.Dir(resolvPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(resolvPath, []byte("nameserver 9.9.9.9\n"), 0o644); err != nil {
		t.Fatalf("seed resolv.conf: %v", err)
	}

	_, err := b.Apply(context.Background(), testTunnel("dnsns", "[Interface]\nPrivateKey = x\nAddress = 10.20.0.2/32\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n"), state.TunnelState{})
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if _, err := os.Stat(resolvPath); !os.IsNotExist(err) {
		t.Fatalf("expected resolv.conf to be removed, err=%v", err)
	}
}
