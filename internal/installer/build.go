package installer

import (
	"fmt"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/model"
)

func InferExistingSnapshot(cfg config.Config) ExistingSnapshot {
	s := ExistingSnapshot{ByURL: map[string]ExistingSubscription{}}
	for feedLabel, feedCfg := range cfg.Feeds {
		if len(feedCfg.Sync.Endpoints) == 0 {
			continue
		}
		url := strings.TrimSpace(feedCfg.Sync.Endpoints[0])
		if url == "" {
			continue
		}
		es := ExistingSubscription{
			Label:            feedLabel,
			URL:              url,
			EnabledByBackend: map[config.BackendType][]string{},
		}
		for _, backendCfg := range feedCfg.Backends {
			enabled := make([]string, 0, len(backendCfg.Tunnels))
			for tunnelID, tCfg := range backendCfg.Tunnels {
				if tCfg.Enabled != nil && *tCfg.Enabled {
					enabled = append(enabled, tunnelID)
				}
			}
			es.EnabledByBackend[backendCfg.Type] = dedupeStrings(enabled)
		}
		s.ByURL[url] = es
	}
	return s
}

func BuildConfig(plan InstallPlan, feedDocs map[string]model.FeedDocument) (config.Config, error) {
	statePath := strings.TrimSpace(plan.StatePath)
	if statePath == "" {
		statePath = DefaultStatePath
	}
	if len(plan.Subscriptions) == 0 {
		return config.Config{}, fmt.Errorf("at least one subscription is required")
	}

	cfg := config.Config{
		StatePath: statePath,
		Feeds:     map[string]config.FeedConfig{},
	}

	for i, sub := range plan.Subscriptions {
		label := normalizeLabel(sub.Label, i)
		if _, exists := cfg.Feeds[label]; exists {
			return config.Config{}, fmt.Errorf("duplicate feed label %q", label)
		}
		url := strings.TrimSpace(sub.URL)
		if url == "" {
			return config.Config{}, fmt.Errorf("subscription %q has empty URL", label)
		}
		doc, ok := feedDocs[url]
		if !ok {
			return config.Config{}, fmt.Errorf("subscription %q has no fetched feed document", label)
		}
		tunnelIDs := make([]string, 0, len(doc.Tunnels))
		for _, t := range doc.Tunnels {
			tunnelIDs = append(tunnelIDs, strings.TrimSpace(t.ID))
		}

		backendMap := map[string]config.FeedBackendConfig{}
		for _, b := range sub.Backends {
			backendLabel := string(b.Type)
			if backendLabel == "" {
				return config.Config{}, fmt.Errorf("subscription %q has empty backend type", label)
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

		cfg.Feeds[label] = config.FeedConfig{
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
		}
	}

	return cfg, nil
}

func HasNetNSBackend(plan InstallPlan) bool {
	for _, sub := range plan.Subscriptions {
		for _, b := range sub.Backends {
			if b.Type == config.BackendNetNS {
				return true
			}
		}
	}
	return false
}

func BuildSystemdUnit(configPath string, hasNetNS bool) string {
	caps := "CAP_NET_ADMIN"
	privateTmp := "true"
	if hasNetNS {
		caps = "CAP_NET_ADMIN CAP_SYS_ADMIN"
		privateTmp = "false"
	}
	return fmt.Sprintf(`[Unit]
Description=wg-feed daemon
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
StateDirectory=wg-feed
PrivateTmp=%s

Environment=LOG_LEVEL=info
ExecStart=/usr/local/bin/wg-feed-daemon --config %s
Restart=always
RestartSec=2s

AmbientCapabilities=%s
CapabilityBoundingSet=%s
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, privateTmp, configPath, caps, caps)
}
