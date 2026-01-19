package stringsx

import "testing"

func TestSplitCommaSeparated(t *testing.T) {
	got := SplitCommaSeparated(" a, b ,, ,c,")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("unexpected result: %#v", got)
	}

	got = SplitCommaSeparated("   ")
	if len(got) != 0 {
		t.Fatalf("unexpected result: %#v", got)
	}
}
