package installer

import (
	"fmt"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/namegen"
)

type TunnelChoice struct {
	Provided bool
	IDs      []string
}

type BackendPlan struct {
	Type           config.BackendType
	EnabledTunnels TunnelChoice
}

type SubscriptionPlan struct {
	Label    string
	URL      string
	Backends []BackendPlan
}

type InstallPlan struct {
	StatePath     string
	Subscriptions []SubscriptionPlan
}

type ExistingSubscription struct {
	Label            string
	URL              string
	EnabledByBackend map[config.BackendType][]string
}

type ExistingSnapshot struct {
	ByURL map[string]ExistingSubscription
}

func normalizeLabel(label string, idx int) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Sprintf("sub-%d", idx+1)
	}
	return label
}

func nextSubscriptionLabel(preferred string, occupied map[string]struct{}) string {
	seed := strings.TrimSpace(preferred)
	if seed == "" {
		seed = "main"
	}
	label, err := namegen.EffectiveName(seed, func(candidate string) (bool, error) {
		_, ok := occupied[candidate]
		return ok, nil
	})
	if err != nil {
		return seed
	}
	return label
}

func NextSubscriptionLabel(preferred string, occupied map[string]struct{}) string {
	return nextSubscriptionLabel(preferred, occupied)
}

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
