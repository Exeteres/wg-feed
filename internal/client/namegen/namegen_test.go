package namegen

import "testing"

func TestEffectiveName_EmptyRequested(t *testing.T) {
	_, err := EffectiveName("   ", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestEffectiveName_NoCollision_ReturnsRequested(t *testing.T) {
	got, err := EffectiveName("tunnel", func(_ string) (bool, error) {
		return false, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tunnel" {
		t.Fatalf("got %q want %q", got, "tunnel")
	}
}

func TestEffectiveName_Collision_AppendsIncrementingSuffix(t *testing.T) {
	occupied := map[string]struct{}{
		"tunnel":   {},
		"tunnel-1": {},
	}
	got, err := EffectiveName("tunnel", func(candidate string) (bool, error) {
		_, ok := occupied[candidate]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tunnel-2" {
		t.Fatalf("got %q want %q", got, "tunnel-2")
	}
}

func TestEffectiveName_ExistingNumericSuffix_ReusesSequence(t *testing.T) {
	occupied := map[string]struct{}{
		"tunnel-2": {},
	}
	got, err := EffectiveName("tunnel-2", func(candidate string) (bool, error) {
		_, ok := occupied[candidate]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tunnel-3" {
		t.Fatalf("got %q want %q", got, "tunnel-3")
	}
}

func TestEffectiveName_ExistingNumericSuffixWithoutDash_ReusesSequence(t *testing.T) {
	occupied := map[string]struct{}{
		"tunnel2": {},
	}
	got, err := EffectiveName("tunnel2", func(candidate string) (bool, error) {
		_, ok := occupied[candidate]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tunnel3" {
		t.Fatalf("got %q want %q", got, "tunnel3")
	}
}

func TestEffectiveName_NumericSuffixNotOccupied_StaysSame(t *testing.T) {
	got, err := EffectiveName("tunnel-2", func(_ string) (bool, error) {
		return false, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tunnel-2" {
		t.Fatalf("got %q want %q", got, "tunnel-2")
	}
}

func TestEffectiveName_ZeroPaddedSuffix_ContinuesNumerically(t *testing.T) {
	occupied := map[string]struct{}{
		"tunnel099": {},
	}
	got, err := EffectiveName("tunnel099", func(candidate string) (bool, error) {
		_, ok := occupied[candidate]
		return ok, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tunnel100" {
		t.Fatalf("got %q want %q", got, "tunnel100")
	}
}
