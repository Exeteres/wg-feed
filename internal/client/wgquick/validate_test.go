package wgquick

import "testing"

func TestValidateRequired_OK(t *testing.T) {
	cfg := Config{
		Interface: Interface{PrivateKey: "x"},
		Peers:     []Peer{{PublicKey: "p"}},
	}
	if err := ValidateRequired(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRequired_MissingPrivateKey(t *testing.T) {
	cfg := Config{Peers: []Peer{{PublicKey: "p"}}}
	if err := ValidateRequired(cfg); err != ErrMissingPrivateKey {
		t.Fatalf("expected ErrMissingPrivateKey, got %v", err)
	}
}

func TestValidateRequired_MissingPeer(t *testing.T) {
	cfg := Config{Interface: Interface{PrivateKey: "x"}}
	if err := ValidateRequired(cfg); err != ErrMissingPeer {
		t.Fatalf("expected ErrMissingPeer, got %v", err)
	}
}
