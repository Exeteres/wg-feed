package client

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/backend"
	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/feed"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/model"
)

func RunOnce(ctx context.Context, cfg config.Config, setupURLs []string, logger *log.Logger) error {
	b, err := backend.New(cfg, logger)
	if err != nil {
		return err
	}

	st, err := state.Load(cfg.StatePath)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	seen := map[string]string{} // feedID -> setupURL
	for _, setupURL := range setupURLs {
		if err := applyOne(ctx, cfg, b, &st, setupURL, logger, seen); err != nil {
			return err
		}
	}

	if err := state.SaveAtomic(cfg.StatePath, st); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

func applyOne(ctx context.Context, cfg config.Config, b backend.Backend, st *state.State, setupURL string, logger *log.Logger, seen map[string]string) error {
	setupURL = strings.TrimSpace(setupURL)

	// Prefer endpoints learned from cached encrypted_data before attempting a bootstrap fetch.
	key, err := st.SubscriptionURLKey(setupURL)
	if err != nil {
		return fmt.Errorf("feed %s: %w", feed.RedactURL(setupURL), err)
	}
	var endpoints []string
	var cachedFeedID string
	if feedID := strings.TrimSpace(st.SetupURLMap[key]); feedID != "" {
		cachedFeedID = feedID
		if fs, ok := st.Feeds[feedID]; ok {
			if strings.TrimSpace(fs.CachedEncryptedData) != "" {
				doc, err := feed.DecryptFeedDocumentForSetupURL(setupURL, fs.CachedEncryptedData)
				if err != nil {
					return fmt.Errorf("feed %s: %w", feed.RedactURL(setupURL), err)
				}
				endpoints = st.OrderEndpoints(feedID, doc.Endpoints)
			}
		}
	}

	// One-shot apply is always a forced reconciliation: it MUST fetch a full document.
	// If endpoints are known, do not use the Setup URL for network requests.
	var res feed.FetchResult
	if len(endpoints) != 0 {
		fetched, usedEndpoint, err := feed.FetchAnyEndpoints(ctx, endpoints, setupURL, "")
		if err != nil {
			return fmt.Errorf("feed %s: %w", feed.RedactURL(setupURL), err)
		}
		res = fetched
		// Best-effort: record endpoint preference for next sync.
		if strings.TrimSpace(cachedFeedID) != "" {
			st.ReconcileEndpointOrder(cachedFeedID, res.Feed.Endpoints, usedEndpoint)
		}
	} else {
		fetched, err := feed.FetchWithDecryptURL(ctx, setupURL, setupURL, "")
		if err != nil {
			return fmt.Errorf("feed %s: %w", feed.RedactURL(setupURL), err)
		}
		res = fetched
	}

	feedID := strings.TrimSpace(res.Feed.ID)
	if feedID == "" {
		return fmt.Errorf("feed %s: missing id", feed.RedactURL(setupURL))
	}
	if msg := strings.TrimSpace(res.Feed.Warning); msg != "" {
		logger.Printf("feed warning: feed=%q message=%q", feed.RedactURL(setupURL), msg)
	}
	if existingURL, ok := seen[feedID]; ok {
		if existingURL != setupURL {
			logger.Printf("duplicate setup url ignored: feed_id=%q url=%q already_seen_at=%q", feedID, feed.RedactURL(setupURL), feed.RedactURL(existingURL))
		}
		return nil
	}
	seen[feedID] = setupURL
	st.SetupURLMap[key] = feedID
	fs := st.Feeds[feedID]
	if fs.Tunnels == nil {
		fs.Tunnels = map[string]state.TunnelState{}
	}
	v := res.TTLSeconds
	fs.TTLSeconds = &v
	if res.Encrypted {
		fs.CachedEncryptedData = strings.TrimSpace(res.EncryptedData)
	} else {
		fs.CachedEncryptedData = ""
	}
	st.Feeds[feedID] = fs

	if err := ApplyFeed(ctx, cfg, b, st, setupURL, res.Feed, logger); err != nil {
		return err
	}
	fs = st.Feeds[feedID]
	fs.LastReconciledRevision = strings.TrimSpace(res.Revision)
	st.Feeds[feedID] = fs
	return nil
}

func ApplyFeed(ctx context.Context, _ config.Config, b backend.Backend, st *state.State, sourceURL string, f model.FeedDocument, logger *log.Logger) error {
	feedID := strings.TrimSpace(f.ID)
	if feedID == "" {
		return fmt.Errorf("feed %s: missing id", feed.RedactURL(sourceURL))
	}
	prev := st.Feeds[feedID]
	if prev.Tunnels == nil {
		prev.Tunnels = map[string]state.TunnelState{}
	}

	currentTunnelIDs := make(map[string]struct{}, len(f.Tunnels))
	for _, t := range f.Tunnels {
		currentTunnelIDs[t.ID] = struct{}{}

		prevTunnel, hadPrev := prev.Tunnels[t.ID]
		enabled := t.Enabled
		if hadPrev && !t.Forced {
			// When forced=false, enabled is only the initial default.
			// Subsequent changes from the feed must be ignored.
			enabled = prevTunnel.Enabled
		}

		// If the backend name hint changes for a managed tunnel, best-effort recreate.
		if hadPrev && strings.TrimSpace(prevTunnel.Name) != "" && strings.TrimSpace(prevTunnel.Name) != strings.TrimSpace(t.Name) {
			_ = b.Remove(ctx, prevTunnel.Name)
			delete(prev.Tunnels, t.ID)
			hadPrev = false
		}

		if err := b.Apply(ctx, t.Name, t.WGQuickConfig, enabled); err != nil {
			logger.Printf("apply failed source=%q tunnel=%q name=%q enabled=%v err=%v", feed.RedactURL(sourceURL), t.ID, t.Name, enabled, err)
			return err
		}
		prev.Tunnels[t.ID] = state.TunnelState{Name: t.Name, Enabled: enabled}
	}

	// Reconcile: tunnels previously seen but missing now are removed.
	for tunnelID, ts := range prev.Tunnels {
		if _, ok := currentTunnelIDs[tunnelID]; ok {
			continue
		}
		if err := b.Remove(ctx, ts.Name); err != nil {
			logger.Printf("remove failed source=%q tunnel=%q name=%q err=%v", feed.RedactURL(sourceURL), tunnelID, ts.Name, err)
		}
		delete(prev.Tunnels, tunnelID)
	}

	st.Feeds[feedID] = prev
	return nil
}
