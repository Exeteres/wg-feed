package nmconfig

import (
	"strings"
	"testing"
)

func TestParse_Empty(t *testing.T) {
	f, err := Parse(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil {
		t.Fatalf("expected file")
	}
}

func TestSetGetAndBytes(t *testing.T) {
	f := NewEmpty()
	f.Set("wireguard", "private-key", "abc")
	got, ok := f.Get("wireguard", "private-key")
	if !ok || got != "abc" {
		t.Fatalf("unexpected get: ok=%v got=%q", ok, got)
	}
	b := f.Bytes()
	if len(b) == 0 || b[len(b)-1] != '\n' {
		t.Fatalf("expected trailing newline")
	}
}

func TestRemoveSectionsWithPrefix(t *testing.T) {
	f, err := Parse([]byte("[a]\nx=1\n[a-1]\ny=2\n[b]\nz=3\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f.RemoveSectionsWithPrefix("a")
	if f.HasSection("a") || f.HasSection("a-1") {
		t.Fatalf("expected sections removed")
	}
	if !f.HasSection("b") {
		t.Fatalf("expected section b to remain")
	}
}

func TestBytes_DoesNotBacktickQuoteSemicolonValues(t *testing.T) {
	f := NewEmpty()
	f.Set("ipv4", "dns", "1.1.1.1;8.8.8.8;")

	b := string(f.Bytes())
	if len(b) == 0 {
		t.Fatalf("expected non-empty output")
	}
	if strings.Contains(b, "`") {
		t.Fatalf("expected output to not contain backticks, got: %q", b)
	}
	if !strings.Contains(b, "dns=1.1.1.1;8.8.8.8;\n") {
		t.Fatalf("expected semicolon value preserved, got: %q", b)
	}
}

func TestParse_IgnoreInlineComment_PreservesSemicolons(t *testing.T) {
	input := "[wireguard-peer.ABC]\nallowed-ips=10.0.0.0/24;10.0.1.0/24;\n"
	f, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := f.Get("wireguard-peer.ABC", "allowed-ips")
	if !ok {
		t.Fatalf("expected key to exist")
	}
	if got != "10.0.0.0/24;10.0.1.0/24;" {
		t.Fatalf("unexpected value: %q", got)
	}
}
