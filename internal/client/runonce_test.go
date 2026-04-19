package client

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/state"
)

func testClientLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunOnce_SkipsSyncDisabledFeedsAndPersistsState(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "state.json")
	cfg := config.Config{
		StatePath: statePath,
		Feeds: map[string]config.FeedConfig{
			"disabled-feed": {
				Sync: config.FeedSyncConfig{Enabled: false},
				Backends: map[string]config.FeedBackendConfig{
					"wg": {Type: config.BackendWGQuick},
				},
			},
		},
	}

	if err := RunOnce(context.Background(), cfg, testClientLogger()); err != nil {
		t.Fatalf("RunOnce error: %v", err)
	}

	st, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("Load state error: %v", err)
	}
	if len(st.Feeds) != 0 {
		t.Fatalf("expected no feed state when all feeds are sync-disabled, got %d entries", len(st.Feeds))
	}
	if st.StateID == "" {
		t.Fatalf("expected non-empty state_id to be persisted")
	}
}

func TestRunOnce_UnknownBackendTypeReturnsError(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "state.json")
	cfg := config.Config{
		StatePath: statePath,
		Feeds: map[string]config.FeedConfig{
			"feed-1": {
				Sync: config.FeedSyncConfig{Enabled: true},
				Backends: map[string]config.FeedBackendConfig{
					"bad": {Type: config.BackendType("invalid")},
				},
			},
		},
	}

	err := RunOnce(context.Background(), cfg, testClientLogger())
	if err == nil {
		t.Fatalf("expected RunOnce to fail for unknown backend type")
	}
	if got, want := err.Error(), "unknown backend"; !strings.Contains(got, want) {
		t.Fatalf("expected error to contain %q, got %q", want, got)
	}
}

func TestBuildBackendSet_UnknownBackendTypeReturnsError(t *testing.T) {
	t.Parallel()

	_, err := buildBackendSet(config.FeedConfig{
		Backends: map[string]config.FeedBackendConfig{
			"bad": {Type: config.BackendType("invalid")},
		},
	}, testClientLogger())
	if err == nil {
		t.Fatalf("expected buildBackendSet to fail for unknown backend type")
	}
}

func TestResolveTunnelEnabled_Precedence(t *testing.T) {
	t.Parallel()

	if got := resolveTunnelEnabled(false, config.FeedTunnelConfig{}, config.FeedTunnelConfig{}); got != false {
		t.Fatalf("expected default doc value false, got %v", got)
	}
	if got := resolveTunnelEnabled(false, config.FeedTunnelConfig{Enabled: boolPtr(true)}, config.FeedTunnelConfig{}); got != true {
		t.Fatalf("expected feed override to win over doc value, got %v", got)
	}
	if got := resolveTunnelEnabled(true, config.FeedTunnelConfig{Enabled: boolPtr(false)}, config.FeedTunnelConfig{Enabled: boolPtr(true)}); got != true {
		t.Fatalf("expected backend override to win over feed override, got %v", got)
	}
}
