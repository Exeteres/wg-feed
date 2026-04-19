package wgquick

import (
	"net/netip"
	"slices"
	"strings"
)

func AllowedIPPrefixSet(cfg Config) map[netip.Prefix]struct{} {
	out := map[netip.Prefix]struct{}{}
	for _, p := range cfg.Peers {
		for _, raw := range p.AllowedIPs {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			pref, err := netip.ParsePrefix(raw)
			if err != nil {
				continue
			}
			out[pref] = struct{}{}
		}
	}
	return out
}

func AllowedIPPrefixes(cfg Config) []netip.Prefix {
	set := AllowedIPPrefixSet(cfg)
	out := make([]netip.Prefix, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b netip.Prefix) int { return strings.Compare(a.String(), b.String()) })
	return out
}

func AllowedIPPrefixSetFromText(conf string) (map[netip.Prefix]struct{}, error) {
	cfg, err := Parse([]byte(conf))
	if err != nil {
		return nil, err
	}
	return AllowedIPPrefixSet(cfg), nil
}
