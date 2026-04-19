package installer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/feed"
	"github.com/exeteres/wg-feed/internal/model"
)

func FetchFeedDocument(ctx context.Context, subscriptionURL string) (model.FeedDocument, error) {
	res, err := feed.FetchWithDecryptURL(ctx, subscriptionURL, subscriptionURL, "")
	if err != nil {
		return model.FeedDocument{}, err
	}
	return res.Feed, nil
}

func BuildFeedConfig(subscriptionURL string, backends []BackendPlan, doc model.FeedDocument) (config.FeedConfig, error) {
	url := strings.TrimSpace(subscriptionURL)
	if url == "" {
		return config.FeedConfig{}, fmt.Errorf("subscription URL is empty")
	}
	if len(backends) == 0 {
		return config.FeedConfig{}, fmt.Errorf("at least one backend is required")
	}

	tunnelIDs := make([]string, 0, len(doc.Tunnels))
	for _, t := range doc.Tunnels {
		tunnelIDs = append(tunnelIDs, strings.TrimSpace(t.ID))
	}
	tunnelIDs = dedupeStrings(tunnelIDs)

	backendMap := map[string]config.FeedBackendConfig{}
	for _, b := range backends {
		backendLabel := string(b.Type)
		if backendLabel == "" {
			return config.FeedConfig{}, fmt.Errorf("backend type is empty")
		}
		enabledSet := map[string]struct{}{}
		for _, id := range b.EnabledTunnels.IDs {
			enabledSet[strings.TrimSpace(id)] = struct{}{}
		}

		tunnelCfg := map[string]config.FeedTunnelConfig{}
		for _, tunnelID := range tunnelIDs {
			if !b.EnabledTunnels.Provided {
				tunnelCfg[tunnelID] = config.FeedTunnelConfig{Enabled: nil}
				continue
			}
			_, enabled := enabledSet[tunnelID]
			v := enabled
			tunnelCfg[tunnelID] = config.FeedTunnelConfig{Enabled: &v}
		}
		backendMap[backendLabel] = config.FeedBackendConfig{Type: b.Type, Tunnels: tunnelCfg}
	}

	return config.FeedConfig{
		Sync: config.FeedSyncConfig{
			Enabled: true,
			Mode:    config.SyncModeSSE,
			Polling: config.FeedPollingSyncConfig{Interval: 0},
			Endpoints: []string{
				url,
			},
		},
		Backends: backendMap,
		Tunnels:  map[string]config.FeedTunnelConfig{},
	}, nil
}

func ConfigFeedBackends(fc config.FeedConfig) []config.BackendType {
	set := map[config.BackendType]struct{}{}
	for _, b := range fc.Backends {
		switch b.Type {
		case config.BackendNetworkManager, config.BackendWGQuick, config.BackendNetNS:
			set[b.Type] = struct{}{}
		}
	}
	out := make([]config.BackendType, 0, len(set))
	for bt := range set {
		out = append(out, bt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func EnabledTunnelIDs(fc config.FeedConfig, backend config.BackendType) []string {
	for _, b := range fc.Backends {
		if b.Type != backend {
			continue
		}
		ids := make([]string, 0, len(b.Tunnels))
		for tunnelID, tunnelCfg := range b.Tunnels {
			if tunnelCfg.Enabled != nil && *tunnelCfg.Enabled {
				ids = append(ids, tunnelID)
			}
		}
		sort.Strings(ids)
		return ids
	}
	return nil
}

func BackendPlansFromFeedConfig(fc config.FeedConfig) []BackendPlan {
	backends := ConfigFeedBackends(fc)
	out := make([]BackendPlan, 0, len(backends))
	for _, bt := range backends {
		provided := false
		ids := []string{}
		for _, b := range fc.Backends {
			if b.Type != bt {
				continue
			}
			for tunnelID, tunnelCfg := range b.Tunnels {
				if tunnelCfg.Enabled != nil {
					provided = true
					if *tunnelCfg.Enabled {
						ids = append(ids, tunnelID)
					}
				}
			}
			break
		}
		ids = dedupeStrings(ids)
		sort.Strings(ids)
		out = append(out, BackendPlan{
			Type: bt,
			EnabledTunnels: TunnelChoice{
				Provided: provided,
				IDs:      ids,
			},
		})
	}
	return out
}
