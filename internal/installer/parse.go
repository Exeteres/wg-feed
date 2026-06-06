package installer

import (
	"fmt"
	"strings"

	"github.com/exeteres/wg-feed/internal/client/config"
)

func ParseURLs(input string) ([]string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("at least one subscription URL is required")
	}
	parts := strings.Fields(strings.ReplaceAll(input, ",", " "))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "https://") {
			return nil, fmt.Errorf("subscription URL must start with https://: %q", p)
		}
		out = append(out, p)
	}
	out = dedupeStrings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one subscription URL is required")
	}
	return out, nil
}

func ParseBackendsInput(input string) ([]config.BackendType, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("at least one backend is required")
	}
	parts := strings.Fields(strings.ReplaceAll(input, ",", " "))
	out := make([]config.BackendType, 0, len(parts))
	seen := map[config.BackendType]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		bt := config.BackendType(p)
		switch bt {
		case config.BackendNetworkManager, config.BackendWGQuick, config.BackendNetNS, config.BackendWindows, config.BackendWindowsManager:
		default:
			return nil, fmt.Errorf("invalid backend %q (allowed: networkmanager, wg-quick, netns, windows, windows-manager)", p)
		}
		if _, ok := seen[bt]; ok {
			continue
		}
		seen[bt] = struct{}{}
		out = append(out, bt)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one backend is required")
	}
	return out, nil
}

func ParseTunnelInput(input string, availableIDs []string) (TunnelChoice, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return TunnelChoice{Provided: false, IDs: nil}, nil
	}
	if strings.EqualFold(input, "all") {
		return TunnelChoice{Provided: true, IDs: dedupeStrings(availableIDs)}, nil
	}
	if strings.EqualFold(input, "none") {
		return TunnelChoice{Provided: true, IDs: []string{}}, nil
	}

	available := map[string]struct{}{}
	for _, id := range availableIDs {
		available[strings.TrimSpace(id)] = struct{}{}
	}

	parts := strings.Fields(strings.ReplaceAll(input, ",", " "))
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := available[p]; !ok {
			return TunnelChoice{}, fmt.Errorf("unknown tunnel id %q", p)
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return TunnelChoice{Provided: true, IDs: out}, nil
}
