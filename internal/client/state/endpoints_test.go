package state

import (
	"testing"
)

func TestCanonicalSubscriptionURLNoFragment_DropsFragmentAndNormalizes(t *testing.T) {
	got, err := CanonicalSubscriptionURLNoFragment("HTTPS://EXAMPLE.COM:443/path?x=1#1kyhr0slrn9cdp6q")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://example.com:443/path?x=1"
	if got != want {
		t.Fatalf("canonical url mismatch: got %q want %q", got, want)
	}
}

func TestEndpointKey_StableAndIgnoresFragment(t *testing.T) {
	st := &State{StateID: "11111111-1111-4111-8111-111111111111"}

	u1 := "https://example.com/sub?id=123#aaa"
	u2 := "https://example.com/sub?id=123#bbb"

	k1, err := st.EndpointKey(u1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	k2, err := st.EndpointKey(u2)
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
