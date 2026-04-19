package wgquick

import (
	"net/netip"
	"testing"
)

func TestAllowedIPPrefixes_DedupAndSorted(t *testing.T) {
	cfg := Config{Peers: []Peer{
		{AllowedIPs: []string{"10.1.0.0/24", "10.0.0.0/24"}},
		{AllowedIPs: []string{"10.1.0.0/24", "2001:db8::/64"}},
	}}

	got := AllowedIPPrefixes(cfg)
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
		netip.MustParsePrefix("10.1.0.0/24"),
		netip.MustParsePrefix("2001:db8::/64"),
	}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d mismatch: got=%s want=%s", i, got[i], want[i])
		}
	}
}

func TestAllowedIPPrefixSetFromText(t *testing.T) {
	conf := "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 10.0.0.0/24, invalid\n"
	set, err := AllowedIPPrefixSetFromText(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(set) != 1 {
		t.Fatalf("unexpected set size: %d", len(set))
	}
	if _, ok := set[netip.MustParsePrefix("10.0.0.0/24")]; !ok {
		t.Fatalf("expected prefix not found")
	}
}
