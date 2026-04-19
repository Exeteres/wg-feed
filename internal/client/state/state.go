package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type State struct {
	// StateID is a per-installation UUIDv4 used to derive stable endpoint hash keys
	// without storing cleartext endpoint URLs.
	StateID string               `json:"state_id,omitempty"`
	Feeds   map[string]FeedState `json:"feeds"`
}

type FeedState struct {
	// ID is the remote feed document id discovered for this configured feed label.
	ID string `json:"id,omitempty"`

	LastReconciledRevision string                  `json:"last_reconciled_revision,omitempty"`
	TTLSeconds             *int                    `json:"ttl_seconds,omitempty"`
	CachedEncryptedData    string                  `json:"cached_encrypted_data,omitempty"`
	EndpointOrder          []string                `json:"endpoint_order,omitempty"` // salted hashes; preferred endpoints first
	Backends               map[string]BackendState `json:"backends"`
}

type BackendState struct {
	Type    string                 `json:"type,omitempty"`
	Tunnels map[string]TunnelState `json:"tunnels"`
}

type TunnelState struct {
	Data json.RawMessage `json:"data,omitempty"`
}

func Load(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			st := State{Feeds: map[string]FeedState{}}
			if err := st.ensureStateIDOnLoad(); err != nil {
				return State{}, err
			}
			return st, nil
		}
		return State{}, err
	}

	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, err
	}
	if st.Feeds == nil {
		st.Feeds = map[string]FeedState{}
	}
	if err := st.ensureStateIDOnLoad(); err != nil {
		return State{}, err
	}
	for feedLabel, fs := range st.Feeds {
		if fs.Backends == nil {
			fs.Backends = map[string]BackendState{}
		}
		for backendLabel, bs := range fs.Backends {
			if bs.Tunnels == nil {
				bs.Tunnels = map[string]TunnelState{}
			}
			fs.Backends[backendLabel] = bs
		}
		st.Feeds[feedLabel] = fs
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

func (st *State) ensureStateIDOnLoad() error {
	id := strings.TrimSpace(st.StateID)
	if id == "" {
		u, err := uuid.NewRandom()
		if err != nil {
			return err
		}
		st.StateID = u.String()
		return nil
	}

	normalizedID, err := validateStateID(id)
	if err != nil {
		return err
	}
	st.StateID = normalizedID
	return nil
}

func validateStateID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("missing state_id")
	}
	u, err := uuid.Parse(id)
	if err != nil {
		return "", fmt.Errorf("invalid state_id: %w", err)
	}
	if u.Version() != 4 {
		return "", fmt.Errorf("invalid state_id: expected UUIDv4")
	}
	return u.String(), nil
}
