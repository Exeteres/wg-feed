package client

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/backend"
	"github.com/exeteres/wg-feed/internal/client/backend/shared"
	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/feed"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/model"
)

func RunOnce(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	st, err := state.Load(cfg.StatePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	logger.Info("apply run started", "state_path", cfg.StatePath, "feeds", len(cfg.Feeds))

	feedLabels := make([]string, 0, len(cfg.Feeds))
	for feedLabel := range cfg.Feeds {
		feedLabels = append(feedLabels, feedLabel)
	}
	sort.Strings(feedLabels)

	seen := map[string]string{} // feedID -> configured feed label
	for _, feedLabel := range feedLabels {
		feedCfg := cfg.Feeds[feedLabel]
		if !feedCfg.Sync.Enabled {
			logger.Debug("feed skipped", "feed_label", feedLabel, "reason", "sync disabled")
			continue
		}

		backendSet, err := buildBackendSet(feedCfg, logger)
		if err != nil {
			return fmt.Errorf("feed %s: %w", feedLabel, err)
		}
		if err := applyOne(ctx, &st, feedLabel, feedCfg, backendSet, logger, seen); err != nil {
			return err
		}
	}

	if err := state.SaveAtomic(cfg.StatePath, st); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	logger.Info("apply run completed")
	return nil
}

func buildBackendSet(feedCfg config.FeedConfig, logger *slog.Logger) (map[string]backend.Backend, error) {
	out := make(map[string]backend.Backend, len(feedCfg.Backends))
	for backendLabel, backendCfg := range feedCfg.Backends {
		b, err := backend.NewForType(backendCfg.Type, logger)
		if err != nil {
			return nil, fmt.Errorf("backend %s: %w", backendLabel, err)
		}
		out[backendLabel] = b
	}
	return out, nil
}

func applyOne(ctx context.Context, st *state.State, feedLabel string, feedCfg config.FeedConfig, backendSet map[string]backend.Backend, logger *slog.Logger, seen map[string]string) error {
	var endpoints []string
	decryptURL := feedCfg.DecryptURL()
	fs := st.Feeds[feedLabel]
	if strings.TrimSpace(fs.CachedEncryptedData) != "" {
		doc, err := feed.DecryptFeedDocumentForURL(decryptURL, fs.CachedEncryptedData)
		if err != nil {
			return fmt.Errorf("feed %s: %w", feedLabel, err)
		}
		endpoints = st.OrderEndpoints(feedLabel, doc.Endpoints)
	}
	if len(endpoints) == 0 {
		endpoints = st.OrderEndpoints(feedLabel, feedCfg.Sync.Endpoints)
	}

	res, usedEndpoint, err := feed.FetchAnyEndpoints(ctx, endpoints, decryptURL, "")
	if err != nil {
		return fmt.Errorf("feed %s: %w", feedLabel, err)
	}
	logger.Debug("feed fetched", "feed_label", feedLabel, "revision", strings.TrimSpace(res.Revision), "encrypted", res.Encrypted, "ttl_seconds", res.TTLSeconds, "endpoint", feed.RedactURL(usedEndpoint))

	feedID := strings.TrimSpace(res.Feed.ID)
	if feedID == "" {
		return fmt.Errorf("feed %s: missing id", feedLabel)
	}
	if msg := strings.TrimSpace(res.Feed.Warning); msg != "" {
		logger.Warn("feed warning", "feed_label", feedLabel, "message", msg)
	}
	if existingLabel, ok := seen[feedID]; ok {
		if existingLabel != feedLabel {
			logger.Info("duplicate feed ignored", "feed_id", feedID, "feed_label", feedLabel, "already_seen_at", existingLabel)
		}
		return nil
	}
	seen[feedID] = feedLabel

	fs = st.Feeds[feedLabel]
	if fs.Backends == nil {
		fs.Backends = map[string]state.BackendState{}
	}
	fs.ID = feedID
	st.ReconcileEndpointOrder(feedLabel, res.Feed.Endpoints, usedEndpoint)
	fs = st.Feeds[feedLabel]
	v := res.TTLSeconds
	fs.TTLSeconds = &v
	if res.Encrypted {
		fs.CachedEncryptedData = strings.TrimSpace(res.EncryptedData)
	} else {
		fs.CachedEncryptedData = ""
	}
	for backendLabel, backendCfg := range feedCfg.Backends {
		bs := fs.Backends[backendLabel]
		if bs.Tunnels == nil {
			bs.Tunnels = map[string]state.TunnelState{}
		}
		bs.Type = string(backendCfg.Type)
		fs.Backends[backendLabel] = bs
	}
	st.Feeds[feedLabel] = fs

	if err := ApplyFeed(ctx, st, feedLabel, backendSet, res.Feed, feedCfg.Tunnels, feedCfg.BackendTunnelOverrides(), logger); err != nil {
		return err
	}
	fs = st.Feeds[feedLabel]
	fs.LastReconciledRevision = strings.TrimSpace(res.Revision)
	st.Feeds[feedLabel] = fs
	return nil
}

func ApplyFeed(ctx context.Context, st *state.State, feedLabel string, backends map[string]backend.Backend, f model.FeedDocument, tunnelCfg map[string]config.FeedTunnelConfig, backendTunnelCfg map[string]map[string]config.FeedTunnelConfig, logger *slog.Logger) error {
	feedID := strings.TrimSpace(f.ID)
	if feedID == "" {
		return fmt.Errorf("feed %s: missing id", feedLabel)
	}

	fs := st.Feeds[feedLabel]
	if fs.Backends == nil {
		fs.Backends = map[string]state.BackendState{}
	}
	fs.ID = feedID

	for backendLabel, b := range backends {
		logger.Debug("reconcile backend started", "feed_label", feedLabel, "backend_label", backendLabel, "tunnels", len(f.Tunnels))
		bs := fs.Backends[backendLabel]
		if bs.Tunnels == nil {
			bs.Tunnels = map[string]state.TunnelState{}
		}
		backendOverrides := backendTunnelCfg[backendLabel]
		currentTunnelIDs := make(map[string]struct{}, len(f.Tunnels))
		for _, t := range f.Tunnels {
			currentTunnelIDs[t.ID] = struct{}{}

			prevTunnel := bs.Tunnels[t.ID]
			enabled := resolveTunnelEnabled(t.Enabled, tunnelCfg[t.ID], backendOverrides[t.ID])

			nextTunnel := prevTunnel
			resolvedTunnel := shared.ResolvedTunnel{Tunnel: t, EffectiveEnabled: enabled}

			appliedTunnel, err := b.Apply(ctx, resolvedTunnel, nextTunnel)
			if err != nil {
				logger.Error("tunnel apply failed", "feed_label", feedLabel, "backend_label", backendLabel, "tunnel_id", t.ID, "enabled", enabled, "err", err)
				return err
			}
			bs.Tunnels[t.ID] = appliedTunnel
		}

		for tunnelID := range bs.Tunnels {
			if _, ok := currentTunnelIDs[tunnelID]; ok {
				continue
			}
			if err := b.Remove(ctx, bs.Tunnels[tunnelID]); err != nil {
				logger.Warn("tunnel remove failed", "feed_label", feedLabel, "backend_label", backendLabel, "tunnel_id", tunnelID, "err", err)
			}
			delete(bs.Tunnels, tunnelID)
		}

		fs.Backends[backendLabel] = bs
	}

	st.Feeds[feedLabel] = fs
	return nil
}

func resolveTunnelEnabled(docEnabled bool, feedOverride config.FeedTunnelConfig, backendOverride config.FeedTunnelConfig) bool {
	enabled := docEnabled
	if feedOverride.Enabled != nil {
		enabled = *feedOverride.Enabled
	}
	if backendOverride.Enabled != nil {
		enabled = *backendOverride.Enabled
	}
	return enabled
}
