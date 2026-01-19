package stringsx

import "strings"

// SplitCommaSeparated splits a comma-separated string, trimming whitespace and
// dropping empty items.
func SplitCommaSeparated(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
