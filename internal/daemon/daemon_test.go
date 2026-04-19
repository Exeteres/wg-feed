package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/exeteres/wg-feed/internal/client/backend"
	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/model"
)

type fakeBackend struct {
	applyCalls []applyCall
	applyErr   error
}

type applyCall struct {
	Name      string
	Config    string
	Enabled   bool
	Forced    bool
	Exclusive bool
}

func (b *fakeBackend) Apply(_ context.Context, tunnel shared.ResolvedTunnel, state state.TunnelState) (state.TunnelState, error) {
	b.applyCalls = append(b.applyCalls, applyCall{Name: tunnel.ID, Config: tunnel.WGQuickConfig, Enabled: tunnel.EffectiveEnabled, Forced: tunnel.Forced, Exclusive: tunnel.Exclusive})
	if b.applyErr != nil {
		return state, b.applyErr
	}
	return state, nil
}

func (b *fakeBackend) Remove(_ context.Context, _ state.TunnelState) error { return nil }

func newRuntime(label, decryptURL string, b backend.Backend) daemonFeed {
	return daemonFeed{
		label:      label,
		decryptURL: decryptURL,
		cfg: config.FeedConfig{
			Sync: config.FeedSyncConfig{
				Mode:      config.SyncModeSSE,
				Endpoints: []string{decryptURL},
			},
			Backends: map[string]config.FeedBackendConfig{
				"b1": {Type: config.BackendWGQuick},
			},
		},
		backends: map[string]backend.Backend{"b1": b},
	}
}

func TestApplyRemoteUpdate_RevisionUnchanged_DoesNotReconcile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")
	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"
	decryptURL := "https://example.test/feed#abc"

	st := state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{
		ID:                     feedID,
		LastReconciledRevision: "rev-1",
		Backends:               map[string]state.BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{}}},
	}
	if err := state.SaveAtomic(statePath, st); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{}
	rf := newRuntime(feedLabel, decryptURL, b)
	d := &daemon{cfg: config.Config{StatePath: statePath}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels:     []model.Tunnel{},
	}

	if err := d.applyRemoteUpdate(ctx, rf, decryptURL, doc, "rev-1", 60, ""); err != nil {
		t.Fatalf("applyRemoteUpdate: %v", err)
	}

	if len(b.applyCalls) != 0 {
		t.Fatalf("expected no apply calls, got %d", len(b.applyCalls))
	}
}

func TestApplyRemoteUpdate_RevisionUnchanged_WithTunnelOverride_Reconciles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")
	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"
	decryptURL := "https://example.test/feed#abc"

	st := state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{
		ID:                     feedID,
		LastReconciledRevision: "rev-1",
		Backends:               map[string]state.BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{"t1": {}}}},
	}
	if err := state.SaveAtomic(statePath, st); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{}
	rf := newRuntime(feedLabel, decryptURL, b)
	enabled := true
	rf.cfg.Tunnels = map[string]config.FeedTunnelConfig{"t1": {Enabled: &enabled}}
	d := &daemon{cfg: config.Config{StatePath: statePath}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
			Forced:        false,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	if err := d.applyRemoteUpdate(ctx, rf, decryptURL, doc, "rev-1", 60, ""); err != nil {
		t.Fatalf("applyRemoteUpdate: %v", err)
	}

	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call, got %d", len(b.applyCalls))
	}
	if got := b.applyCalls[0].Enabled; !got {
		t.Fatalf("expected enabled override=true in apply call")
	}
	if got := b.applyCalls[0].Forced; got != false {
		t.Fatalf("expected forced to preserve server value false, got %v", got)
	}
}

func TestApplyRemoteUpdate_RevisionUnchanged_WithBackendTunnelOverride_Reconciles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")
	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"
	decryptURL := "https://example.test/feed#abc"

	st := state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{
		ID:                     feedID,
		LastReconciledRevision: "rev-1",
		Backends:               map[string]state.BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{"t1": {}}}},
	}
	if err := state.SaveAtomic(statePath, st); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{}
	rf := newRuntime(feedLabel, decryptURL, b)
	enabled := true
	rf.cfg.Backends["b1"] = config.FeedBackendConfig{
		Type:    config.BackendWGQuick,
		Tunnels: map[string]config.FeedTunnelConfig{"t1": {Enabled: &enabled}},
	}
	d := &daemon{cfg: config.Config{StatePath: statePath}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
			Forced:        false,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	if err := d.applyRemoteUpdate(ctx, rf, decryptURL, doc, "rev-1", 60, ""); err != nil {
		t.Fatalf("applyRemoteUpdate: %v", err)
	}

	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call, got %d", len(b.applyCalls))
	}
	if got := b.applyCalls[0].Enabled; !got {
		t.Fatalf("expected enabled override=true in apply call")
	}
}

func TestApplyRemoteUpdate_ReconcileFailure_DoesNotAdvanceLastReconciledRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")
	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"
	decryptURL := "https://example.test/feed#abc"

	st := state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{ID: feedID, Backends: map[string]state.BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{}}}}
	if err := state.SaveAtomic(statePath, st); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{applyErr: errors.New("boom")}
	rf := newRuntime(feedLabel, decryptURL, b)
	d := &daemon{cfg: config.Config{StatePath: statePath}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
			Forced:        true,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	if err := d.applyRemoteUpdate(ctx, rf, decryptURL, doc, "rev-2", 60, ""); err == nil {
		t.Fatalf("expected error")
	}

	st2, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := strings.TrimSpace(st2.Feeds[feedLabel].LastReconciledRevision); got != "" {
		t.Fatalf("expected last_reconciled_revision unchanged, got %q", got)
	}
}

func TestApplyRemoteUpdate_EmptyState_InitializesMaps(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")
	feedLabel := "main"
	feedID := "11111111-1111-4111-8111-111111111111"
	decryptURL := "https://example.test/feed#abc"

	if err := state.SaveAtomic(statePath, state.State{}); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{}
	rf := newRuntime(feedLabel, decryptURL, b)
	d := &daemon{cfg: config.Config{StatePath: statePath}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
			Forced:        true,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	if err := d.applyRemoteUpdate(ctx, rf, decryptURL, doc, "rev-1", 60, ""); err != nil {
		t.Fatalf("applyRemoteUpdate: %v", err)
	}

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.TrimSpace(st.Feeds[feedLabel].ID) != feedID {
		t.Fatalf("feed id mismatch: got %q want %q", st.Feeds[feedLabel].ID, feedID)
	}
	if _, ok := st.Feeds[feedLabel].Backends["b1"]; !ok {
		t.Fatalf("expected backend state for b1")
	}
}

func TestMaybeReconcileFromCache_NoCache_DoesNotApply(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")
	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"
	decryptURL := "https://example.test/feed#abc"

	st := state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{ID: feedID, Backends: map[string]state.BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{}}}}
	if err := state.SaveAtomic(statePath, st); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{}
	rf := newRuntime(feedLabel, decryptURL, b)
	d := &daemon{cfg: config.Config{StatePath: statePath}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	if err := d.maybeReconcileFromCache(ctx, rf, feedID, time.Time{}); err == nil {
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
	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	fragment := strings.ToLower(strings.TrimPrefix(id.String(), "AGE-SECRET-KEY-"))
	decryptURL := "https://example.test/feed#" + fragment

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
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

	st := state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{
		ID:                  feedID,
		CachedEncryptedData: buf.String(),
		Backends:            map[string]state.BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{}}},
	}
	if err := state.SaveAtomic(statePath, st); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{}
	rf := newRuntime(feedLabel, decryptURL, b)
	d := &daemon{cfg: config.Config{StatePath: statePath}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	if err := d.maybeReconcileFromCache(ctx, rf, feedID, time.Time{}); err != nil {
		t.Fatalf("maybeReconcileFromCache: %v", err)
	}
	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call got %d", len(b.applyCalls))
	}
}

func TestMaybeReconcileFromCache_WithTunnelOverride_AppliesOverride(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "state.json")
	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("GenerateX25519Identity: %v", err)
	}
	fragment := strings.ToLower(strings.TrimPrefix(id.String(), "AGE-SECRET-KEY-"))
	decryptURL := "https://example.test/feed#" + fragment

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
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

	st := state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{
		ID:                  feedID,
		CachedEncryptedData: buf.String(),
		Backends:            map[string]state.BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{"t1": {}}}},
	}
	if err := state.SaveAtomic(statePath, st); err != nil {
		t.Fatalf("SaveAtomic: %v", err)
	}

	b := &fakeBackend{}
	rf := newRuntime(feedLabel, decryptURL, b)
	enabled := true
	rf.cfg.Tunnels = map[string]config.FeedTunnelConfig{"t1": {Enabled: &enabled}}
	d := &daemon{cfg: config.Config{StatePath: statePath}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	if err := d.maybeReconcileFromCache(ctx, rf, feedID, time.Time{}); err != nil {
		t.Fatalf("maybeReconcileFromCache: %v", err)
	}
	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call got %d", len(b.applyCalls))
	}
	if got := b.applyCalls[0].Enabled; !got {
		t.Fatalf("expected enabled override=true in apply call")
	}
	if got := b.applyCalls[0].Forced; got != true {
		t.Fatalf("expected forced to preserve server value true, got %v", got)
	}
}

func TestRedactTransportError_RedactsFullURL(t *testing.T) {
	t.Parallel()

	err := errors.New(`Get "https://example.invalid/sub/path?token=secret#frag": dial tcp: i/o timeout`)
	got := redactTransportError(err)

	if strings.Contains(got, "/sub/path") || strings.Contains(got, "token=secret") || strings.Contains(got, "#frag") {
		t.Fatalf("expected path/query/fragment to be redacted, got %q", got)
	}
	if !strings.Contains(got, "https://example.invalid#") {
		t.Fatalf("expected redacted host fingerprint, got %q", got)
	}
}

func TestRedactTransportError_NoURL_Unchanged(t *testing.T) {
	t.Parallel()

	err := errors.New("network timeout")
	got := redactTransportError(err)
	if got != "network timeout" {
		t.Fatalf("expected unchanged error, got %q", got)
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
