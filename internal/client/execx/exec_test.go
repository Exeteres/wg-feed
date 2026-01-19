package execx

import "testing"

func TestShellQuote(t *testing.T) {
	got := shellQuote("wg", []string{"a", "b"})
	if got != "wg a b" {
		t.Fatalf("unexpected: %q", got)
	}

	got = shellQuote("wg", []string{"hello world"})
	if got != "wg \"hello world\"" {
		t.Fatalf("unexpected: %q", got)
	}
}
