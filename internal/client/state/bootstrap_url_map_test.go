package state

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestCanonicalSetupURLNoFragment_DropsFragmentAndNormalizes(t *testing.T) {
	got, err := CanonicalSetupURLNoFragment("HTTPS://EXAMPLE.COM:443/path?x=1#1kyhr0slrn9cdp6q")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://example.com:443/path?x=1"
	if got != want {
		t.Fatalf("canonical url mismatch: got %q want %q", got, want)
	}
}

func TestSetupURLKey_StableAndIgnoresFragment(t *testing.T) {
	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}
	st := &State{SetupURLSalt: hex.EncodeToString(salt)}

	u1 := "https://example.com/sub?id=123#aaa"
	u2 := "https://example.com/sub?id=123#bbb"

	k1, err := st.SetupURLKey(u1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	k2, err := st.SetupURLKey(u2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("expected same key for differing fragments: %q vs %q", k1, k2)
	}
	if len(k1) != 64 {
		t.Fatalf("expected 64 hex chars (sha256): got %d", len(k1))
	}
}

func TestSetupURLKey_GeneratesSalt(t *testing.T) {
	st := &State{}
	k1, err := st.SetupURLKey("https://example.com/sub#abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(st.SetupURLSalt) == "" {
		t.Fatalf("expected salt to be generated")
	}
	if _, err := hex.DecodeString(st.SetupURLSalt); err != nil {
		t.Fatalf("expected hex salt: %v", err)
	}
	k2, err := st.SetupURLKey("https://example.com/sub#different")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("expected stable key (fragment ignored): %q vs %q", k1, k2)
	}
}
