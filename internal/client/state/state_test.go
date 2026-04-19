package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
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
	if st.StateID == "" {
		t.Fatalf("expected state_id")
	}
	u, err := uuid.Parse(st.StateID)
	if err != nil {
		t.Fatalf("expected UUID state_id: %v", err)
	}
	if u.Version() != 4 {
		t.Fatalf("expected UUIDv4 state_id, got v%d", u.Version())
	}
}

func TestSaveAtomic_RoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	type tunnelData struct {
		DeviceName string `json:"device_name"`
	}
	dataBytes, err := json.Marshal(tunnelData{DeviceName: "home"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	in := State{Feeds: map[string]FeedState{"abc": {Backends: map[string]BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]TunnelState{"t1": {Data: dataBytes}}}}}}}

	if err := SaveAtomic(path, in); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.StateID == "" {
		t.Fatalf("expected state_id")
	}
	u, err := uuid.Parse(out.StateID)
	if err != nil {
		t.Fatalf("expected UUID state_id: %v", err)
	}
	if u.Version() != 4 {
		t.Fatalf("expected UUIDv4 state_id, got v%d", u.Version())
	}
	var outData tunnelData
	if err := json.Unmarshal(out.Feeds["abc"].Backends["b1"].Tunnels["t1"].Data, &outData); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if outData.DeviceName != "home" {
		t.Fatalf("Tunnel data mismatch")
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(b) == 0 {
		t.Fatalf("expected non-empty file")
	}
}
