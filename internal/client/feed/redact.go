package feed

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// RedactURL returns a stable, non-secret representation of a setup URL for logs.
// It intentionally drops userinfo, path, query, and fragment.
func RedactURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "subscription#empty"
	}

	sum := sha256.Sum256([]byte(raw))
	id := hex.EncodeToString(sum[:4])

	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "subscription#" + id
	}

	host := u.Hostname()
	if host == "" {
		host = u.Host
	}
	return fmt.Sprintf("%s://%s#%s", u.Scheme, host, id)
}
