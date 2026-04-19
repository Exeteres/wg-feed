package state

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// CanonicalSubscriptionURLNoFragment returns a stable string form of a subscription-related URL
// with its fragment removed.
//
// This is used for deriving salted hash keys without persisting the URL itself.
func CanonicalSubscriptionURLNoFragment(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	// Drop fragment (age key lives here and must not be part of the stored derivation input).
	u.Fragment = ""
	u.RawFragment = ""

	// Normalize scheme/host casing for stability.
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if host != "" {
		if port := u.Port(); port != "" {
			u.Host = host + ":" + port
		} else {
			u.Host = host
		}
	}

	return u.String(), nil
}

// EndpointKey returns the stable, salted hash key for a URL.
//
// Callers should pass URLs without fragments when possible; fragments are ignored.
func (st *State) EndpointKey(rawURL string) (string, error) {
	key := []byte(st.StateID)
	canon, err := CanonicalSubscriptionURLNoFragment(rawURL)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canon))
	sum := mac.Sum(nil)
	return hex.EncodeToString(sum), nil
}

// OrderEndpoints returns endpoints ordered by the stored preference list of salted hashes.
// Any endpoints not present in fs.EndpointOrder are appended in their original order.
func (st *State) OrderEndpoints(feedLabel string, endpoints []string) []string {
	feedLabel = strings.TrimSpace(feedLabel)
	if feedLabel == "" {
		return slices.Clip(endpoints)
	}
	fs, ok := st.Feeds[feedLabel]
	if !ok {
		return slices.Clip(endpoints)
	}
	return orderEndpointsByHashes(endpoints, fs.EndpointOrder, st.EndpointKey)
}

func orderEndpointsByHashes(endpoints []string, preferredHashes []string, hashFn func(string) (string, error)) []string {
	trimmed := make([]string, 0, len(endpoints))
	for _, e := range endpoints {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		trimmed = append(trimmed, e)
	}
	if len(trimmed) == 0 || len(preferredHashes) == 0 {
		return trimmed
	}

	h2e := make(map[string]string, len(trimmed))
	seenE := make(map[string]struct{}, len(trimmed))
	for _, e := range trimmed {
		if _, ok := seenE[e]; ok {
			continue
		}
		seenE[e] = struct{}{}
		h, err := hashFn(e)
		if err != nil {
			continue
		}
		// If collisions occur, first one wins (best-effort).
		if _, ok := h2e[h]; !ok {
			h2e[h] = e
		}
	}

	out := make([]string, 0, len(trimmed))
	used := make(map[string]struct{}, len(trimmed))
	for _, h := range preferredHashes {
		e, ok := h2e[strings.TrimSpace(h)]
		if !ok {
			continue
		}
		if _, ok := used[e]; ok {
			continue
		}
		used[e] = struct{}{}
		out = append(out, e)
	}
	for _, e := range trimmed {
		if _, ok := used[e]; ok {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ReconcileEndpointOrder updates an existing stored hash order for a feed given the current
// endpoints list. If promotedEndpoint is non-empty and present in endpoints, it is moved to the front.
func (st *State) ReconcileEndpointOrder(feedLabel string, endpoints []string, promotedEndpoint string) {
	feedLabel = strings.TrimSpace(feedLabel)
	if feedLabel == "" {
		return
	}
	fs := st.Feeds[feedLabel]
	fs.EndpointOrder = reconcileEndpointOrderHashes(fs.EndpointOrder, endpoints, promotedEndpoint, st.EndpointKey)
	st.Feeds[feedLabel] = fs
}

func reconcileEndpointOrderHashes(existing []string, endpoints []string, promotedEndpoint string, hashFn func(string) (string, error)) []string {
	endpointHashes := make([]string, 0, len(endpoints))
	inDoc := map[string]struct{}{}
	for _, e := range endpoints {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		h, err := hashFn(e)
		if err != nil {
			continue
		}
		endpointHashes = append(endpointHashes, h)
		inDoc[h] = struct{}{}
	}
	if len(endpointHashes) == 0 {
		return nil
	}

	var promotedHash string
	if strings.TrimSpace(promotedEndpoint) != "" {
		h, err := hashFn(promotedEndpoint)
		if err == nil {
			if _, ok := inDoc[h]; ok {
				promotedHash = h
			}
		}
	}

	out := make([]string, 0, len(endpointHashes))
	seen := map[string]struct{}{}
	add := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" {
			return
		}
		if _, ok := seen[h]; ok {
			return
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}

	if promotedHash != "" {
		add(promotedHash)
	}
	for _, h := range existing {
		h = strings.TrimSpace(h)
		if _, ok := inDoc[h]; !ok {
			continue
		}
		add(h)
	}
	for _, h := range endpointHashes {
		add(h)
	}
	return out
}
