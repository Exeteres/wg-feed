package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_MissingFile_ReturnsEmptyState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing.json")
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if st.Feeds == nil {
		t.Fatalf("expected non-nil feeds map")
	}
	if len(st.Feeds) != 0 {
		t.Fatalf("expected empty feeds map")
	}
}

func TestSaveAtomic_RoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	in := State{Feeds: map[string]FeedState{"abc": {Tunnels: map[string]TunnelState{"t1": {Name: "home", Enabled: true}}}}}

	if err := SaveAtomic(path, in); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Feeds["abc"].Tunnels["t1"].Name != "home" {
		t.Fatalf("Tunnel name mismatch")
	}
	if !out.Feeds["abc"].Tunnels["t1"].Enabled {
		t.Fatalf("Tunnel enabled mismatch")
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(b) == 0 {
		t.Fatalf("expected non-empty file")
	}
}
