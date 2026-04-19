package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/backend"
	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/model"
)

func boolPtr(v bool) *bool { return &v }

type wgquickTunnelData struct {
	DeviceName string `json:"device_name"`
}

type fakeBackend struct {
	applyCalls  []applyCall
	removeCalls []string
	applyErr    error
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

func (b *fakeBackend) Remove(_ context.Context, state state.TunnelState) error {
	b.removeCalls = append(b.removeCalls, "removed")
	return nil
}

func TestApplyFeed_ForcedFalse_PreservesPreviousEnabled(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	prevData, err := json.Marshal(wgquickTunnelData{DeviceName: "t1"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{
		ID: feedID,
		Backends: map[string]state.BackendState{
			"b1": {
				Type: "wg-quick",
				Tunnels: map[string]state.TunnelState{
					"t1": {Data: prevData},
				},
			},
		},
	}

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

	b := &fakeBackend{}
	backendSet := map[string]backend.Backend{"b1": b}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, nil, nil, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call got %d", len(b.applyCalls))
	}
	if got := b.applyCalls[0].Enabled; got != false {
		t.Fatalf("enabled mismatch: got %v want %v", got, false)
	}
	if got := b.applyCalls[0].Forced; got != false {
		t.Fatalf("expected forced to preserve server value false, got %v", got)
	}
}

func TestApplyFeed_ForcedTrue_UsesFeedEnabled(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{ID: feedID, Backends: map[string]state.BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{}}}}

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
			Exclusive:     true,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	b := &fakeBackend{}
	backendSet := map[string]backend.Backend{"b1": b}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, nil, nil, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call got %d", len(b.applyCalls))
	}
	if got := b.applyCalls[0].Enabled; got != true {
		t.Fatalf("enabled mismatch: got %v want %v", got, true)
	}
	if got := b.applyCalls[0].Exclusive; got != true {
		t.Fatalf("exclusive mismatch: got %v want %v", got, true)
	}
	if got := b.applyCalls[0].Forced; got != true {
		t.Fatalf("expected forced to preserve server value true, got %v", got)
	}
}

func TestApplyFeed_ServerForcedDisabled_PassesForcedTrue(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{ID: feedID, Backends: map[string]state.BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{}}}}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
			Enabled:       false,
			Forced:        true,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	b := &fakeBackend{}
	backendSet := map[string]backend.Backend{"b1": b}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, nil, nil, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call got %d", len(b.applyCalls))
	}
	if got := b.applyCalls[0].Enabled; got != false {
		t.Fatalf("enabled mismatch: got %v want %v", got, false)
	}
	if got := b.applyCalls[0].Forced; got != true {
		t.Fatalf("expected forced=true for server-forced disabled tunnel, got %v", got)
	}
}

func TestApplyFeed_NameHintChange_StillUsesTunnelID(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	prevData, err := json.Marshal(wgquickTunnelData{DeviceName: "t1"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{
		ID: feedID,
		Backends: map[string]state.BackendState{
			"b1": {
				Type: "wg-quick",
				Tunnels: map[string]state.TunnelState{
					"t1": {Data: prevData},
				},
			},
		},
	}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "newname",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
			Forced:        true,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	b := &fakeBackend{}
	backendSet := map[string]backend.Backend{"b1": b}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, nil, nil, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.removeCalls) != 0 {
		t.Fatalf("expected no remove calls, got %#v", b.removeCalls)
	}
	if len(b.applyCalls) != 1 || b.applyCalls[0].Name != "t1" {
		t.Fatalf("expected Apply(t1), got %#v", b.applyCalls)
	}
	var td wgquickTunnelData
	if err := json.Unmarshal(st.Feeds[feedLabel].Backends["b1"].Tunnels["t1"].Data, &td); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if td.DeviceName != "t1" {
		t.Fatalf("device_name mismatch: got %q want %q", td.DeviceName, "t1")
	}
}

func TestApplyFeed_RemovesMissingTunnels(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{
		ID: feedID,
		Backends: map[string]state.BackendState{
			"b1": {
				Type: "wg-quick",
				Tunnels: map[string]state.TunnelState{
					"t1": {},
				},
			},
		},
	}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels:     []model.Tunnel{},
	}

	b := &fakeBackend{}
	backendSet := map[string]backend.Backend{"b1": b}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, nil, nil, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.removeCalls) != 1 {
		t.Fatalf("expected one remove call, got %#v", b.removeCalls)
	}
	if len(st.Feeds[feedLabel].Backends["b1"].Tunnels) != 0 {
		t.Fatalf("expected state tunnels cleared")
	}
}

func TestApplyFeed_BackendApplyError_Propagates(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{Backends: map[string]state.BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{}}}}
	doc := model.FeedDocument{
		ID:          "11111111-1111-4111-8111-111111111111",
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

	b := &fakeBackend{applyErr: errors.New("boom")}
	backendSet := map[string]backend.Backend{"b1": b}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, nil, nil, logger); err == nil {
		t.Fatalf("expected error")
	}
}

func TestApplyFeed_NetworkManager_UsesTunnelID(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{ID: feedID, Backends: map[string]state.BackendState{"nm": {Type: "networkmanager", Tunnels: map[string]state.TunnelState{}}}}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{
			{ID: "t1", Name: "home", DisplayInfo: model.DisplayInfo{Title: "Home"}, Forced: true, WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n"},
			{ID: "t2", Name: "home", DisplayInfo: model.DisplayInfo{Title: "Home2"}, Forced: true, WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n"},
		},
	}

	b := &fakeBackend{}
	backendSet := map[string]backend.Backend{"nm": b}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, nil, nil, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.applyCalls) != 2 {
		t.Fatalf("expected 2 apply calls got %d", len(b.applyCalls))
	}
	if b.applyCalls[0].Name != "t1" || b.applyCalls[1].Name != "t2" {
		t.Fatalf("unexpected apply identifiers: %#v", b.applyCalls)
	}

	t1 := st.Feeds[feedLabel].Backends["nm"].Tunnels["t1"]
	t2 := st.Feeds[feedLabel].Backends["nm"].Tunnels["t2"]
	if len(t1.Data) != 0 || len(t2.Data) != 0 {
		t.Fatalf("expected generic client to not populate backend-specific data")
	}
}

func TestApplyFeed_Windows_UsesTunnelID(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{ID: feedID, Backends: map[string]state.BackendState{"w": {Type: "windows", Tunnels: map[string]state.TunnelState{}}}}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "office",
			DisplayInfo:   model.DisplayInfo{Title: "Office"},
			Forced:        true,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	b := &fakeBackend{}
	backendSet := map[string]backend.Backend{"w": b}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, nil, nil, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.applyCalls) != 1 || b.applyCalls[0].Name != "t1" {
		t.Fatalf("unexpected apply calls: %#v", b.applyCalls)
	}
	if len(st.Feeds[feedLabel].Backends["w"].Tunnels["t1"].Data) != 0 {
		t.Fatalf("expected generic client to not populate backend-specific data")
	}
}

func TestApplyFeed_TunnelConfigEnabledOverride_WinsOverServerForced(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{ID: feedID, Backends: map[string]state.BackendState{"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{}}}}

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

	b := &fakeBackend{}
	backendSet := map[string]backend.Backend{"b1": b}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tunnelCfg := map[string]config.FeedTunnelConfig{"t1": {Enabled: boolPtr(false)}}

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, tunnelCfg, nil, logger); err != nil {
		t.Fatalf("applyFeedWithTunnelConfig: %v", err)
	}

	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call got %d", len(b.applyCalls))
	}
	if got := b.applyCalls[0].Enabled; got != false {
		t.Fatalf("enabled mismatch: got %v want %v", got, false)
	}
	if got := b.applyCalls[0].Forced; got != true {
		t.Fatalf("expected forced=true for explicit disabled override, got %v", got)
	}
}

func TestApplyFeed_TunnelConfigEnabledOverride_WinsOverStateAndServer(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{
		ID: feedID,
		Backends: map[string]state.BackendState{
			"b1": {
				Type: "wg-quick",
				Tunnels: map[string]state.TunnelState{
					"t1": {},
				},
			},
		},
	}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
			Enabled:       false,
			Forced:        false,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	b := &fakeBackend{}
	backendSet := map[string]backend.Backend{"b1": b}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tunnelCfg := map[string]config.FeedTunnelConfig{"t1": {Enabled: boolPtr(true)}}

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, tunnelCfg, nil, logger); err != nil {
		t.Fatalf("applyFeedWithTunnelConfig: %v", err)
	}

	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call got %d", len(b.applyCalls))
	}
	if got := b.applyCalls[0].Enabled; got != true {
		t.Fatalf("enabled mismatch: got %v want %v", got, true)
	}
	if got := b.applyCalls[0].Forced; got != false {
		t.Fatalf("forced should be ignored and passed as false, got %v", got)
	}
}

func TestApplyFeed_BackendTunnelOverride_WinsOverFeedTunnelOverride(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{
		ID: feedID,
		Backends: map[string]state.BackendState{
			"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{}},
		},
	}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
			Enabled:       false,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	b := &fakeBackend{}
	backendSet := map[string]backend.Backend{"b1": b}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	feedTunnelCfg := map[string]config.FeedTunnelConfig{"t1": {Enabled: boolPtr(false)}}
	backendTunnelCfg := map[string]map[string]config.FeedTunnelConfig{"b1": {"t1": {Enabled: boolPtr(true)}}}

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, feedTunnelCfg, backendTunnelCfg, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call got %d", len(b.applyCalls))
	}
	if got := b.applyCalls[0].Enabled; got != true {
		t.Fatalf("enabled mismatch: got %v want %v", got, true)
	}
}

func TestApplyFeed_BackendTunnelOverride_IsBackendSpecific(t *testing.T) {
	t.Parallel()

	feedLabel := "feed1"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedLabel] = state.FeedState{
		ID: feedID,
		Backends: map[string]state.BackendState{
			"b1": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{}},
			"b2": {Type: "wg-quick", Tunnels: map[string]state.TunnelState{}},
		},
	}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels: []model.Tunnel{{
			ID:            "t1",
			Name:          "home",
			DisplayInfo:   model.DisplayInfo{Title: "Home"},
			Enabled:       false,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	b1 := &fakeBackend{}
	b2 := &fakeBackend{}
	backendSet := map[string]backend.Backend{"b1": b1, "b2": b2}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	backendTunnelCfg := map[string]map[string]config.FeedTunnelConfig{"b1": {"t1": {Enabled: boolPtr(true)}}}

	if err := ApplyFeed(context.Background(), st, feedLabel, backendSet, doc, nil, backendTunnelCfg, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b1.applyCalls) != 1 || len(b2.applyCalls) != 1 {
		t.Fatalf("expected one apply call for each backend")
	}
	if got := b1.applyCalls[0].Enabled; got != true {
		t.Fatalf("backend b1 enabled mismatch: got %v want %v", got, true)
	}
	if got := b2.applyCalls[0].Enabled; got != false {
		t.Fatalf("backend b2 enabled mismatch: got %v want %v", got, false)
	}
}
