package nmconfig

import "testing"

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
