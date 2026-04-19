package wgquick

import (
	"regexp"
	"strings"
)

var amneziaKeyRe = regexp.MustCompile(`(?i)^\s*(i[1-5]|s[1-4]|jc|jmin|jmax|h[1-4])\s*=`)

// HasAmneziaExtensions reports whether wg-quick text contains Amnezia-specific keys.
func HasAmneziaExtensions(wgQuickConfig string) bool {
	for _, line := range strings.Split(wgQuickConfig, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if amneziaKeyRe.MatchString(trimmed) {
			return true
		}
	}
	return false
}
