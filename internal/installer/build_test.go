package installer

import (
	"strings"
	"testing"

	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/model"
)

func TestBuildConfig_DefinedByServerTunnelBehavior(t *testing.T) {
	plan := InstallPlan{
		StatePath: "/var/lib/wg-feed/state.json",
		Subscriptions: []SubscriptionPlan{
			{
				Label: "sub-1",
				URL:   "https://a.example/sub#key",
				Backends: []BackendPlan{
					{Type: config.BackendWGQuick, EnabledTunnels: TunnelChoice{Provided: false}},
					{Type: config.BackendNetNS, EnabledTunnels: TunnelChoice{Provided: false}},
				},
			},
		},
	}
	docs := map[string]model.FeedDocument{
		"https://a.example/sub#key": {
			ID: "feed-1",
			Tunnels: []model.Tunnel{
				{ID: "t1", Name: "n1", DisplayInfo: model.DisplayInfo{Title: "T1"}, WGQuickConfig: "x"},
				{ID: "t2", Name: "n2", DisplayInfo: model.DisplayInfo{Title: "T2"}, WGQuickConfig: "x"},
			},
		},
	}

	cfg, err := BuildConfig(plan, docs)
	if err != nil {
		t.Fatalf("BuildConfig error: %v", err)
	}

	wg := cfg.Feeds["sub-1"].Backends["wg-quick"]
	ns := cfg.Feeds["sub-1"].Backends["netns"]

	if wg.Tunnels["t1"].Enabled != nil || wg.Tunnels["t2"].Enabled != nil {
		t.Fatalf("wg-quick defined-by-server should omit enabled field")
	}
	if ns.Tunnels["t1"].Enabled != nil || ns.Tunnels["t2"].Enabled != nil {
		t.Fatalf("netns defined-by-server should omit enabled field")
	}
}

func TestBuildSystemdUnit_NetNS(t *testing.T) {
	unit := BuildSystemdUnit("/etc/wg-feed/config.yaml", true)
	if !strings.Contains(unit, "CAP_NET_ADMIN CAP_SYS_ADMIN") {
		t.Fatalf("expected CAP_SYS_ADMIN for netns")
	}
	if !strings.Contains(unit, "PrivateTmp=false") {
		t.Fatalf("expected PrivateTmp=false for netns")
	}
}
