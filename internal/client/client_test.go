package client

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/model"
)

type fakeBackend struct {
	applyCalls  []applyCall
	removeCalls []string
	applyErr    error
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

func (b *fakeBackend) Remove(_ context.Context, name string) error {
	b.removeCalls = append(b.removeCalls, name)
	return nil
}

func TestApplyFeed_ForcedFalse_PreservesPreviousEnabled(t *testing.T) {
	t.Parallel()

	setupURL := "https://example.test/feed"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedID] = state.FeedState{
		Tunnels: map[string]state.TunnelState{
			"t1": {Name: "home", Enabled: false},
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
			Enabled:       true,  // should be ignored
			Forced:        false, // keep prior
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	b := &fakeBackend{}
	logger := log.New(io.Discard, "", 0)

	if err := ApplyFeed(context.Background(), config.Config{}, b, st, setupURL, doc, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call got %d", len(b.applyCalls))
	}
	if got := b.applyCalls[0].Enabled; got != false {
		t.Fatalf("enabled mismatch: got %v want %v", got, false)
	}
	if got := st.Feeds[feedID].Tunnels["t1"].Enabled; got != false {
		t.Fatalf("state enabled mismatch: got %v want %v", got, false)
	}
}

func TestApplyFeed_ForcedTrue_UsesFeedEnabled(t *testing.T) {
	t.Parallel()

	setupURL := "https://example.test/feed"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedID] = state.FeedState{Tunnels: map[string]state.TunnelState{}}

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

	b := &fakeBackend{}
	logger := log.New(io.Discard, "", 0)

	if err := ApplyFeed(context.Background(), config.Config{}, b, st, setupURL, doc, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.applyCalls) != 1 {
		t.Fatalf("expected 1 apply call got %d", len(b.applyCalls))
	}
	if got := b.applyCalls[0].Enabled; got != true {
		t.Fatalf("enabled mismatch: got %v want %v", got, true)
	}
	if got := st.Feeds[feedID].Tunnels["t1"].Enabled; got != true {
		t.Fatalf("state enabled mismatch: got %v want %v", got, true)
	}
}

func TestApplyFeed_NameChange_RemovesOldNameThenAppliesNew(t *testing.T) {
	t.Parallel()

	setupURL := "https://example.test/feed"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedID] = state.FeedState{
		Tunnels: map[string]state.TunnelState{
			"t1": {Name: "oldname", Enabled: true},
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
			Enabled:       true,
			Forced:        true,
			WGQuickConfig: "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nAllowedIPs = 0.0.0.0/0\n",
		}},
	}

	b := &fakeBackend{}
	logger := log.New(io.Discard, "", 0)

	if err := ApplyFeed(context.Background(), config.Config{}, b, st, setupURL, doc, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.removeCalls) != 1 || b.removeCalls[0] != "oldname" {
		t.Fatalf("expected Remove(oldname), got %#v", b.removeCalls)
	}
	if len(b.applyCalls) != 1 || b.applyCalls[0].Name != "newname" {
		t.Fatalf("expected Apply(newname), got %#v", b.applyCalls)
	}
	if got := st.Feeds[feedID].Tunnels["t1"].Name; got != "newname" {
		t.Fatalf("state name mismatch: got %q want %q", got, "newname")
	}
}

func TestApplyFeed_RemovesMissingTunnels(t *testing.T) {
	t.Parallel()

	setupURL := "https://example.test/feed"
	feedID := "11111111-1111-4111-8111-111111111111"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	st.Feeds[feedID] = state.FeedState{
		Tunnels: map[string]state.TunnelState{
			"t1": {Name: "home", Enabled: true},
		},
	}

	doc := model.FeedDocument{
		ID:          feedID,
		Endpoints:   []string{"https://example.test/feed"},
		DisplayInfo: model.DisplayInfo{Title: "Example"},
		Tunnels:     []model.Tunnel{},
	}

	b := &fakeBackend{}
	logger := log.New(io.Discard, "", 0)

	if err := ApplyFeed(context.Background(), config.Config{}, b, st, setupURL, doc, logger); err != nil {
		t.Fatalf("ApplyFeed: %v", err)
	}

	if len(b.removeCalls) != 1 || b.removeCalls[0] != "home" {
		t.Fatalf("expected Remove(home), got %#v", b.removeCalls)
	}
	if len(st.Feeds[feedID].Tunnels) != 0 {
		t.Fatalf("expected state tunnels cleared")
	}
}

func TestApplyFeed_BackendApplyError_Propagates(t *testing.T) {
	t.Parallel()

	setupURL := "https://example.test/feed"

	st := &state.State{Feeds: map[string]state.FeedState{}}
	doc := model.FeedDocument{
		ID:          "11111111-1111-4111-8111-111111111111",
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

	b := &fakeBackend{applyErr: errors.New("boom")}
	logger := log.New(io.Discard, "", 0)

	if err := ApplyFeed(context.Background(), config.Config{}, b, st, setupURL, doc, logger); err == nil {
		t.Fatalf("expected error")
	}
}
