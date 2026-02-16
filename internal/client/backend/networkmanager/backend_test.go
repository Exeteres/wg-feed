package networkmanager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/backend/networkmanager/nmconfig"
	"github.com/exeteres/wg-feed/internal/client/execx"
	"github.com/exeteres/wg-feed/internal/client/wgquick"
	"gopkg.in/ini.v1"
)

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

func TestBuildNMConnection_PreservesUUIDAndProxySection(t *testing.T) {
	existing := []byte(`
[connection]
id=old
uuid=6dd51d78-a6f0-4f58-87eb-4f1c699199af
type=wireguard
interface-name=old

[proxy]
method=auto
`)

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

	out, err := buildNMConnection(existing, "amsterdam-2", parsed, func() string { return "NEWUUID" })
	if err != nil {
		t.Fatalf("buildNMConnection error: %v", err)
	}

	f, err := nmconfig.Parse(out)
	if err != nil {
		t.Fatalf("nmconfig parse: %v", err)
	}

	if got, _ := f.Get("connection", "uuid"); got != "6dd51d78-a6f0-4f58-87eb-4f1c699199af" {
		t.Fatalf("uuid not preserved: %q", got)
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

	// Ensure unrelated existing section survives.
	if got, _ := f.Get("proxy", "method"); got != "auto" {
		t.Fatalf("proxy section not preserved: %q", got)
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
	b := New(r, nil)
	b.nmDir = nmDir

	if err := b.Apply(context.Background(), "amsterdam-2", config, true, true); err != nil {
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
}

func TestApply_Enabled_ForcedFalse_DoesNotAutostart(t *testing.T) {
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
	b := New(r, nil)
	b.nmDir = nmDir

	if err := b.Apply(context.Background(), "amsterdam-2", config, true, false); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if strings.Contains(joined, "nmcli connection up id amsterdam-2") {
		t.Fatalf("did not expect up call when forced=false; got:\n%s", joined)
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
	b := New(r, nil)
	b.nmDir = nmDir

	if err := b.Apply(context.Background(), "amsterdam-2", config, false, true); err != nil {
		t.Fatalf("Apply error: %v", err)
	}

	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "nmcli connection reload") {
		t.Fatalf("expected reload call; got:\n%s", joined)
	}
	if !strings.Contains(joined, "nmcli connection down id amsterdam-2") {
		t.Fatalf("expected down call; got:\n%s", joined)
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
	b := New(r, nil)
	b.nmDir = nmDir

	if err := b.Apply(context.Background(), "amsterdam", config, false, true); err != nil {
		t.Fatalf("Apply error: %v", err)
	}
}
