package networkmanager

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/backend/networkmanager/nmconfig"
	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/execx"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/client/wgquick"
	"github.com/exeteres/wg-feed/internal/model"
	"gopkg.in/ini.v1"
)

func testTunnel(name, cfg string, forced bool, exclusive bool) shared.ResolvedTunnel {
	return shared.ResolvedTunnel{Tunnel: model.Tunnel{Name: name, WGQuickConfig: cfg, Enabled: true, Forced: forced, Exclusive: exclusive}, EffectiveEnabled: true}
}

func testTunnelDisabled(name, cfg string, forced bool, exclusive bool) shared.ResolvedTunnel {
	return shared.ResolvedTunnel{Tunnel: model.Tunnel{Name: name, WGQuickConfig: cfg, Enabled: false, Forced: forced, Exclusive: exclusive}, EffectiveEnabled: false}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeRunner struct {
	calls []string
	errFn func(call string) error
}

type fakeExitCodeError struct {
	code int
	msg  string
}

func (e *fakeExitCodeError) Error() string { return e.msg }
func (e *fakeExitCodeError) ExitCode() int { return e.code }

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (execx.Result, error) {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if r.errFn != nil {
		if err := r.errFn(call); err != nil {
			return execx.Result{}, err
		}
	}
	return execx.Result{}, nil
}

func TestBuildNMConnection_FromScratch_UsesProvidedUUID(t *testing.T) {
	mtu := 1280
	parsed := wgquick.Config{
		Interface: wgquick.Interface{
			PrivateKey: "PRIVATEKEY",
			Addresses:  []string{"192.168.47.1/32"},
			DNS:        []string{"1.1.1.1"},
			MTU:        &mtu,
		},
		Peers: []wgquick.Peer{{
			PublicKey:    "PUBLICKEY",
			Endpoint:     "endpoint:1234",
			PresharedKey: "PSK",
			AllowedIPs:   []string{"192.168.10.1", "0.0.0.0/0"},
		}},
	}

	out, err := buildNMConnection("amsterdam-2", parsed, provisioningMode{}, true, "fixed-uuid")
	if err != nil {
		t.Fatalf("buildNMConnection error: %v", err)
	}

	f, err := nmconfig.Parse(out)
	if err != nil {
		t.Fatalf("nmconfig parse: %v", err)
	}

	if got, _ := f.Get("connection", "uuid"); got != "fixed-uuid" {
		t.Fatalf("uuid mismatch: %q", got)
	}
	if got, _ := f.Get("connection", "id"); got != "amsterdam-2" {
		t.Fatalf("id mismatch: %q", got)
	}
	if got, _ := f.Get("connection", "interface-name"); got != "amsterdam-2" {
		t.Fatalf("interface-name mismatch: %q", got)
	}
	if got, _ := f.Get("wireguard", "mtu"); got != "1280" {
		t.Fatalf("mtu mismatch: %q", got)
	}
	if got, _ := f.Get("wireguard", "private-key"); got != "PRIVATEKEY" {
		t.Fatalf("private-key mismatch: %q", got)
	}
	if got, _ := f.Get("wireguard-peer.PUBLICKEY", "allowed-ips"); got != "192.168.10.1;0.0.0.0/0;" {
		t.Fatalf("allowed-ips mismatch: %q", got)
	}
	if got, _ := f.Get("ipv4", "address1"); got != "192.168.47.1/32" {
		t.Fatalf("ipv4 address mismatch: %q", got)
	}
	if got, _ := f.Get("ipv4", "dns"); got != "1.1.1.1;" {
		t.Fatalf("dns mismatch: %q", got)
	}

	if _, ok := f.Get("proxy", "method"); ok {
		t.Fatalf("proxy section should not exist in from-scratch build")
	}
}

func TestApply_EnabledCallsReloadAndUp_NoDeleteImport(t *testing.T) {
	tmp := t.TempDir()
	nmDir := filepath.Join(tmp, "nm")

	config := `
[Interface]
PrivateKey = PRIVATEKEY
Address = 192.168.47.1/32
DNS = 1.1.1.1

[Peer]
PublicKey = PUBLICKEY
Endpoint = endpoint:1234
PresharedKey = PSK
AllowedIPs = 192.168.10.1/32, 0.0.0.0/0
`

	r := &fakeRunner{}
	b := New(r, testLogger())
	b.nmDir = nmDir
	b.listPath = filepath.Join(tmp, "exclusive.list")
	b.scriptPath = filepath.Join(tmp, "90-wg-feed-exclusive")

	if _, err := b.Apply(context.Background(), testTunnel("amsterdam-2", config, true, false), state.TunnelState{}); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "nmcli connection reload") {
		t.Fatalf("expected reload call; got:\n%s", joined)
	}
	if !strings.Contains(joined, "nmcli connection up id amsterdam-2") {
		t.Fatalf("expected up call; got:\n%s", joined)
	}
	if strings.Contains(joined, "connection delete") || strings.Contains(joined, "connection import") {
		t.Fatalf("did not expect delete/import; got:\n%s", joined)
	}

	// Confirm nmconnection file written.
	path := filepath.Join(nmDir, "amsterdam-2.nmconnection")
	bts, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected nmconnection file: %v", err)
	}
	f, err := ini.Load(bts)
	if err != nil {
		t.Fatalf("ini load: %v", err)
	}
	if got := f.Section("connection").Key("id").String(); got != "amsterdam-2" {
		t.Fatalf("id mismatch: %q", got)
	}
	if got := f.Section("connection").Key("autoconnect").String(); got != "true" {
		t.Fatalf("autoconnect mismatch: %q", got)
	}
}

func TestApply_Enabled_ForcedFalse_StillAutostarts(t *testing.T) {
	tmp := t.TempDir()
	nmDir := filepath.Join(tmp, "nm")

	config := `
[Interface]
PrivateKey = PRIVATEKEY
Address = 192.168.47.1/32

[Peer]
PublicKey = PUBLICKEY
AllowedIPs = 0.0.0.0/0
`

	r := &fakeRunner{}
	b := New(r, testLogger())
	b.nmDir = nmDir
	b.listPath = filepath.Join(tmp, "exclusive.list")
	b.scriptPath = filepath.Join(tmp, "90-wg-feed-exclusive")

	if _, err := b.Apply(context.Background(), testTunnel("amsterdam-2", config, false, false), state.TunnelState{}); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "nmcli connection up id amsterdam-2") {
		t.Fatalf("expected up call; got:\n%s", joined)
	}

	path := filepath.Join(nmDir, "amsterdam-2.nmconnection")
	bts, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected nmconnection file: %v", err)
	}
	f, err := ini.Load(bts)
	if err != nil {
		t.Fatalf("ini load: %v", err)
	}
	if got := f.Section("connection").Key("autoconnect").String(); got != "true" {
		t.Fatalf("autoconnect mismatch: %q", got)
	}
}

func TestApply_DisabledCallsReloadAndDown(t *testing.T) {
	tmp := t.TempDir()
	nmDir := filepath.Join(tmp, "nm")
	config := `
[Interface]
PrivateKey = PRIVATEKEY
Address = 192.168.47.1/32

[Peer]
PublicKey = PUBLICKEY
AllowedIPs = 0.0.0.0/0
`

	r := &fakeRunner{}
	b := New(r, testLogger())
	b.nmDir = nmDir
	b.listPath = filepath.Join(tmp, "exclusive.list")
	b.scriptPath = filepath.Join(tmp, "90-wg-feed-exclusive")

	if _, err := b.Apply(context.Background(), testTunnelDisabled("amsterdam-2", config, true, false), state.TunnelState{}); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "nmcli connection reload") {
		t.Fatalf("expected reload call; got:\n%s", joined)
	}
	if !strings.Contains(joined, "nmcli connection down id amsterdam-2") {
		t.Fatalf("expected down call; got:\n%s", joined)
	}

	path := filepath.Join(nmDir, "amsterdam-2.nmconnection")
	bts, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected nmconnection file: %v", err)
	}
	f, err := ini.Load(bts)
	if err != nil {
		t.Fatalf("ini load: %v", err)
	}
	if got := f.Section("connection").Key("autoconnect").String(); got != "false" {
		t.Fatalf("autoconnect mismatch: %q", got)
	}
}

func TestApply_Disabled_ForcedFalse_DoesNotDown(t *testing.T) {
	tmp := t.TempDir()
	nmDir := filepath.Join(tmp, "nm")
	config := `
[Interface]
PrivateKey = PRIVATEKEY
Address = 192.168.47.1/32

[Peer]
PublicKey = PUBLICKEY
AllowedIPs = 0.0.0.0/0
`

	r := &fakeRunner{}
	b := New(r, testLogger())
	b.nmDir = nmDir
	b.listPath = filepath.Join(tmp, "exclusive.list")
	b.scriptPath = filepath.Join(tmp, "90-wg-feed-exclusive")

	if _, err := b.Apply(context.Background(), testTunnelDisabled("amsterdam-2", config, false, false), state.TunnelState{}); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "nmcli connection reload") {
		t.Fatalf("expected reload call; got:\n%s", joined)
	}
	if strings.Contains(joined, "nmcli connection down id amsterdam-2") {
		t.Fatalf("did not expect down call when forced=false; got:\n%s", joined)
	}

	path := filepath.Join(nmDir, "amsterdam-2.nmconnection")
	bts, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected nmconnection file: %v", err)
	}
	f, err := ini.Load(bts)
	if err != nil {
		t.Fatalf("ini load: %v", err)
	}
	if got := f.Section("connection").Key("autoconnect").String(); got != "false" {
		t.Fatalf("autoconnect mismatch: %q", got)
	}
}

func TestApply_Disabled_IgnoresNotActiveConnectionError(t *testing.T) {
	tmp := t.TempDir()
	nmDir := filepath.Join(tmp, "nm")
	config := `
[Interface]
PrivateKey = PRIVATEKEY
Address = 192.168.47.1/32

[Peer]
PublicKey = PUBLICKEY
AllowedIPs = 0.0.0.0/0
`

	r := &fakeRunner{errFn: func(call string) error {
		if call == "nmcli connection down id amsterdam" {
			return &fakeExitCodeError{code: 10, msg: "exit status 10"}
		}
		return nil
	}}
	b := New(r, testLogger())
	b.nmDir = nmDir
	b.listPath = filepath.Join(tmp, "exclusive.list")
	b.scriptPath = filepath.Join(tmp, "90-wg-feed-exclusive")

	if _, err := b.Apply(context.Background(), testTunnelDisabled("amsterdam", config, true, false), state.TunnelState{}); err != nil {
		t.Fatalf("Apply error: %v", err)
	}
}

func TestApply_ExclusiveTrue_WritesDispatcherAndList(t *testing.T) {
	tmp := t.TempDir()
	nmDir := filepath.Join(tmp, "nm")
	listPath := filepath.Join(tmp, "exclusive.list")
	scriptPath := filepath.Join(tmp, "90-wg-feed-exclusive")

	config := `
[Interface]
PrivateKey = PRIVATEKEY
Address = 192.168.47.1/32

[Peer]
PublicKey = PUBLICKEY
AllowedIPs = 0.0.0.0/0
`

	r := &fakeRunner{}
	b := New(r, testLogger())
	b.nmDir = nmDir
	b.listPath = listPath
	b.scriptPath = scriptPath

	if _, err := b.Apply(context.Background(), testTunnel("amsterdam-2", config, true, true), state.TunnelState{}); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	listBytes, err := os.ReadFile(listPath)
	if err != nil {
		t.Fatalf("read list: %v", err)
	}
	if got := strings.TrimSpace(string(listBytes)); got != "amsterdam-2" {
		t.Fatalf("exclusive list mismatch: %q", got)
	}

	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	if !strings.Contains(string(scriptBytes), "nmcli connection down id") {
		t.Fatalf("dispatcher script missing down command")
	}
	if !strings.Contains(string(scriptBytes), listPath) {
		t.Fatalf("dispatcher script missing list path")
	}
}

func TestApply_ExclusiveFalse_RemovesRegistrationAndScriptWhenEmpty(t *testing.T) {
	tmp := t.TempDir()
	nmDir := filepath.Join(tmp, "nm")
	listPath := filepath.Join(tmp, "exclusive.list")
	scriptPath := filepath.Join(tmp, "90-wg-feed-exclusive")

	config := `
[Interface]
PrivateKey = PRIVATEKEY
Address = 192.168.47.1/32

[Peer]
PublicKey = PUBLICKEY
AllowedIPs = 0.0.0.0/0
`

	r := &fakeRunner{}
	b := New(r, testLogger())
	b.nmDir = nmDir
	b.listPath = listPath
	b.scriptPath = scriptPath

	ts := state.TunnelState{}
	var err error
	ts, err = b.Apply(context.Background(), testTunnel("amsterdam-2", config, true, true), ts)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}
	_, err = b.Apply(context.Background(), testTunnel("amsterdam-2", config, true, false), ts)
	if err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	if _, err := os.Stat(listPath); !os.IsNotExist(err) {
		t.Fatalf("expected exclusive list removed, err=%v", err)
	}
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Fatalf("expected dispatcher script removed, err=%v", err)
	}
}

func TestApply_Amnezia_GeneratesVPNKeyfile(t *testing.T) {
	tmp := t.TempDir()
	nmDir := filepath.Join(tmp, "nm")

	config := `
[Interface]
PrivateKey = PRIVATEKEY
Address = 10.7.0.2/32
Jc = 3
Jmin = 10
S1 = 12

[Peer]
PublicKey = PUBLICKEY
	PresharedKey = PSKVALUE
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = vpn.example:51820
`

	r := &fakeRunner{}
	b := New(r, testLogger())
	b.nmDir = nmDir
	b.listPath = filepath.Join(t.TempDir(), "exclusive.list")
	b.scriptPath = filepath.Join(t.TempDir(), "90-wg-feed-exclusive")

	if _, err := b.Apply(context.Background(), testTunnel("amz-1", config, true, false), state.TunnelState{}); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if strings.Contains(joined, "connection import") {
		t.Fatalf("did not expect import mode for amnezia; got:\n%s", joined)
	}
	if !strings.Contains(joined, "nmcli connection up id amz-1") {
		t.Fatalf("expected up call; got:\n%s", joined)
	}

	path := filepath.Join(nmDir, "amz-1.nmconnection")
	bts, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected nmconnection file: %v", err)
	}
	f, err := nmconfig.Parse(bts)
	if err != nil {
		t.Fatalf("nmconfig parse: %v", err)
	}
	if got, _ := f.Get("connection", "type"); got != "vpn" {
		t.Fatalf("expected vpn connection type, got %q", got)
	}
	if got, _ := f.Get("vpn", "service-type"); got != "org.freedesktop.NetworkManager.amneziawg" {
		t.Fatalf("unexpected service-type: %q", got)
	}
	if got, _ := f.Get("vpn", "connection-jc"); got != "3" {
		t.Fatalf("unexpected connection-jc: %q", got)
	}
	if got, _ := f.Get("vpn", "connection-jmin"); got != "10" {
		t.Fatalf("unexpected connection-jmin: %q", got)
	}
	if got, _ := f.Get("vpn", "connection-s1"); got != "12" {
		t.Fatalf("unexpected connection-s1: %q", got)
	}
	if got, _ := f.Get("vpn", "peer-0-public-key"); got != "PUBLICKEY" {
		t.Fatalf("unexpected peer public key: %q", got)
	}
	if got, _ := f.Get("vpn", "peer-0-endpoint"); got != "vpn.example:51820" {
		t.Fatalf("unexpected endpoint: %q", got)
	}
	if got, _ := f.Get("vpn", "peer-0-allowed-ips"); got != "0.0.0.0/0,::/0" {
		t.Fatalf("unexpected allowed-ips: %q", got)
	}
	if got, _ := f.Get("ipv4", "route1"); got != "0.0.0.0/0" {
		t.Fatalf("unexpected ipv4 route1: %q", got)
	}
	if got, _ := f.Get("ipv6", "route1"); got != "::/0" {
		t.Fatalf("unexpected ipv6 route1: %q", got)
	}
	if got, _ := f.Get("vpn-secrets", "local-private-key"); got != "PRIVATEKEY" {
		t.Fatalf("unexpected local-private-key secret: %q", got)
	}
	if got, _ := f.Get("vpn", "local-private-key"); got != "" {
		t.Fatalf("local-private-key must not be in vpn section: %q", got)
	}
	if got, _ := f.Get("vpn-secrets", "peer-0-preshared-key"); got != "PSKVALUE" {
		t.Fatalf("unexpected peer-0-preshared-key secret: %q", got)
	}
	if got, _ := f.Get("vpn", "peer-0-preshared-key"); got != "" {
		t.Fatalf("peer-0-preshared-key must not be in vpn section: %q", got)
	}
}

func TestApply_Amnezia_ForcedFalse_StillUpAndAutoconnectTrue(t *testing.T) {
	tmp := t.TempDir()
	nmDir := filepath.Join(tmp, "nm")

	config := `
[Interface]
PrivateKey = PRIVATEKEY
Address = 10.7.0.2/32
Jc = 3

[Peer]
PublicKey = PUBLICKEY
AllowedIPs = 0.0.0.0/0
Endpoint = vpn.example:51820
`

	r := &fakeRunner{}
	b := New(r, testLogger())
	b.nmDir = nmDir
	b.listPath = filepath.Join(t.TempDir(), "exclusive.list")
	b.scriptPath = filepath.Join(t.TempDir(), "90-wg-feed-exclusive")

	if _, err := b.Apply(context.Background(), testTunnel("amz-2", config, false, false), state.TunnelState{}); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if strings.Contains(joined, "connection import") {
		t.Fatalf("did not expect import mode for amnezia; got:\n%s", joined)
	}
	if !strings.Contains(joined, "nmcli connection up id amz-2") {
		t.Fatalf("expected up call; got:\n%s", joined)
	}

	path := filepath.Join(nmDir, "amz-2.nmconnection")
	bts, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected nmconnection file: %v", err)
	}
	f, err := ini.Load(bts)
	if err != nil {
		t.Fatalf("ini load: %v", err)
	}
	if got := f.Section("connection").Key("autoconnect").String(); got != "true" {
		t.Fatalf("autoconnect mismatch: %q", got)
	}
}
