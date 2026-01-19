package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type State struct {
	// SetupURLSalt is a per-installation random salt used to hash Setup URLs
	// (without storing them) into keys for SetupURLMap.
	SetupURLSalt string            `json:"setup_url_salt,omitempty"`
	SetupURLMap  map[string]string `json:"setup_url_map,omitempty"` // hashed canonical setup url (no fragment) -> feed id

	Feeds map[string]FeedState `json:"feeds"`
}

type FeedState struct {
	// Keyed by Feed ID (subscription ID).
	LastReconciledRevision string                 `json:"last_reconciled_revision,omitempty"`
	TTLSeconds             *int                   `json:"ttl_seconds,omitempty"`
	CachedEncryptedData    string                 `json:"cached_encrypted_data,omitempty"`
	Tunnels                map[string]TunnelState `json:"tunnels"`
}

type TunnelState struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func Load(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{SetupURLMap: map[string]string{}, Feeds: map[string]FeedState{}}, nil
		}
		return State{}, err
	}

	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, err
	}
	if st.SetupURLMap == nil {
		st.SetupURLMap = map[string]string{}
	}
	if st.Feeds == nil {
		st.Feeds = map[string]FeedState{}
	}
	return st, nil
}

func SaveAtomic(path string, st State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
