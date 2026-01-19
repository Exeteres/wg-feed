package wgquick

import "testing"

func TestParse_Basic(t *testing.T) {
	cfgText := `
# comment
[Interface]
PrivateKey = priv
Address = 10.0.0.1/32, 10.0.0.2/32
DNS = 1.1.1.1
MTU = 1420

[Peer]
PublicKey = pub1
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25

[Peer]
PublicKey = pub2
Endpoint = example.com:51820
`

	cfg, err := Parse([]byte(cfgText))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Interface.PrivateKey != "priv" {
		t.Fatalf("unexpected private key: %q", cfg.Interface.PrivateKey)
	}
	if len(cfg.Interface.Addresses) != 2 {
		t.Fatalf("unexpected addresses: %#v", cfg.Interface.Addresses)
	}
	if cfg.Interface.MTU == nil || *cfg.Interface.MTU != 1420 {
		t.Fatalf("unexpected mtu: %#v", cfg.Interface.MTU)
	}
	if len(cfg.Peers) != 2 {
		t.Fatalf("unexpected peers: %#v", cfg.Peers)
	}
	if cfg.Peers[0].PublicKey != "pub1" {
		t.Fatalf("unexpected peer 0 pubkey: %q", cfg.Peers[0].PublicKey)
	}
	if cfg.Peers[0].PersistentKeepalive == nil || *cfg.Peers[0].PersistentKeepalive != 25 {
		t.Fatalf("unexpected keepalive: %#v", cfg.Peers[0].PersistentKeepalive)
	}
	if cfg.Peers[1].PublicKey != "pub2" {
		t.Fatalf("unexpected peer 1 pubkey: %q", cfg.Peers[1].PublicKey)
	}
	if cfg.Peers[1].Endpoint != "example.com:51820" {
		t.Fatalf("unexpected endpoint: %q", cfg.Peers[1].Endpoint)
	}
}

func TestParse_InvalidMTU(t *testing.T) {
	_, err := Parse([]byte("[Interface]\nMTU = nope\n"))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestParse_InvalidPersistentKeepalive(t *testing.T) {
	_, err := Parse([]byte("[Peer]\nPersistentKeepalive = nope\n"))
	if err == nil {
		t.Fatalf("expected error")
	}
}
