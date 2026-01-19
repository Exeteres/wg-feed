package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/model"
)

type fakeBackend struct {
	applyCalls []applyCall
	applyErr   error
}

type applyCall struct {
	Name    string
	Config  string
	Enabled bool
}

func (b *fakeBackend) Apply(_ context.Context, name string, wgQuickConfig string, enabled bool) error {
	b.applyCalls = append(b.applyCalls, applyCall{Name: name, Config: wgQuickConfig, Enabled: enabled})
	return b.applyErr
}

func (b *fakeBackend) Remove(_ context.Context, _ string) error { return nil }

func TestApplyRemoteUpdate_RevisionUnchanged_DoesNotReconcile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")

	setupURL := "https://example.test/feed"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedID] = state.FeedState{LastReconciledRevision: "rev-1", Tunnels: map[string]state.TunnelState{}}
	if err := state.SaveAtomic(statePath, st); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{}
	d := &daemon{cfg: config.Config{StatePath: statePath}, b: b, logger: log.New(io.Discard, "", 0)}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels:     []model.Tunnel{},
	}

	if err := d.applyRemoteUpdate(ctx, setupURL, setupURL, doc, "rev-1", 60, ""); err != nil {
		t.Fatalf("applyRemoteUpdate: %v", err)
	}

	if len(b.applyCalls) != 0 {
		t.Fatalf("expected no apply calls, got %d", len(b.applyCalls))
	}
}

func TestApplyRemoteUpdate_ReconcileFailure_DoesNotAdvanceLastReconciledRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")

	setupURL := "https://example.test/feed"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedID] = state.FeedState{Tunnels: map[string]state.TunnelState{}}
	if err := state.SaveAtomic(statePath, st); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{applyErr: errors.New("boom")}
	d := &daemon{cfg: config.Config{StatePath: statePath}, b: b, logger: log.New(io.Discard, "", 0)}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
			Enabled:       true,
			Forced:        true,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	if err := d.applyRemoteUpdate(ctx, setupURL, setupURL, doc, "rev-2", 60, ""); err == nil {
		t.Fatalf("expected error")
	}

	st2, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := strings.TrimSpace(st2.Feeds[feedID].LastReconciledRevision); got != "" {
		t.Fatalf("expected last_reconciled_revision unchanged, got %q", got)
	}
}

func TestMaybeReconcileFromCache_NoCache_DoesNotApply(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")

	setupURL := "https://example.test/feed"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedID] = state.FeedState{Tunnels: map[string]state.TunnelState{}}
	if err := state.SaveAtomic(statePath, st); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{}
	d := &daemon{cfg: config.Config{StatePath: statePath}, b: b, logger: log.New(io.Discard, "", 0)}

	if err := d.maybeReconcileFromCache(ctx, setupURL, feedID, time.Time{}); err == nil {
		t.Fatalf("expected error")
	}
	if len(b.applyCalls) != 0 {
		t.Fatalf("expected no apply calls")
	}
}

func TestMaybeReconcileFromCache_WithEncryptedCache_Applies(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	fragment := strings.ToLower(strings.TrimPrefix(id.String(), "AGE-SECRET-KEY-"))
	setupURL := "https://example.test/feed#" + fragment
	feedID := "11111111-1111-4111-8111-111111111111"

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
			Enabled:       true,
			Forced:        true,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	pt, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, id.Recipient())
	if err != nil {
		_ = aw.Close()
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := w.Write(pt); err != nil {
		_ = w.Close()
		_ = aw.Close()
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		_ = aw.Close()
		t.Fatalf("Close: %v", err)
	}
	if err := aw.Close(); err != nil {
		t.Fatalf("ArmorClose: %v", err)
	}
	enc := buf.String()
	if strings.TrimSpace(enc) == "" {
		t.Fatalf("expected non-empty encrypted data")
	}

	st := state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedID] = state.FeedState{CachedEncryptedData: enc, Tunnels: map[string]state.TunnelState{}}
	if err := state.SaveAtomic(statePath, st); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{}
	d := &daemon{cfg: config.Config{StatePath: statePath}, b: b, logger: log.New(io.Discard, "", 0)}

	if err := d.maybeReconcileFromCache(ctx, setupURL, feedID, time.Time{}); err != nil {
		t.Fatalf("maybeReconcileFromCache: %v", err)
	}
	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call got %d", len(b.applyCalls))
	}
}

func TestLoadVersionlessStateJSON_Succeeds(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"feeds":{}}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := state.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
}
