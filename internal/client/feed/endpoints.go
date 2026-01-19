package feed

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

func randomizedCopy(endpoints []string) []string {
	out := make([]string, 0, len(endpoints))
	for _, e := range endpoints {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		out = append(out, e)
	}
	// Fisher-Yates with crypto/rand.
	for i := len(out) - 1; i > 0; i-- {
		jBig, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			break
		}
		j := int(jBig.Int64())
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// FetchWithDecryptURL fetches requestURL but uses decryptURL (the Setup URL containing the age key
// fragment) for decrypting encrypted_data when present.
func FetchWithDecryptURL(ctx context.Context, requestURL, decryptURL string, ifNoneMatchRevision string) (FetchResult, error) {
	sr, body, notModified, err := fetchSuccessResponse(ctx, requestURL, ifNoneMatchRevision)
	if err != nil {
		return FetchResult{}, err
	}
	if notModified {
		return FetchResult{NotModified: true, Revision: strings.TrimSpace(ifNoneMatchRevision)}, nil
	}

	res := FetchResult{}
	res.Revision = strings.TrimSpace(sr.Revision)
	res.TTLSeconds = sr.TTLSeconds
	res.SupportsSSE = sr.SupportsSSE
	res.Body = body

	if sr.Encrypted {
		doc, err := DecryptFeedDocumentForSetupURL(decryptURL, sr.EncryptedData)
		if err != nil {
			return FetchResult{}, err
		}
		res.Encrypted = true
		res.EncryptedData = sr.EncryptedData
		res.Feed = doc
		return res, nil
	}
	if sr.Data == nil {
		return FetchResult{}, fmt.Errorf("validate response: data is required when encrypted=false")
	}
	res.Encrypted = false
	res.EncryptedData = ""
	res.Feed = *sr.Data
	return res, nil
}

// FetchAnyEndpoints attempts to fetch a feed from endpoints[] in randomized order.
// It returns the first successful result plus the endpoint URL that succeeded.
func FetchAnyEndpoints(ctx context.Context, endpoints []string, decryptURL string, ifNoneMatchRevision string) (FetchResult, string, error) {
	order := randomizedCopy(endpoints)
	if len(order) == 0 {
		return FetchResult{}, "", fmt.Errorf("no endpoints")
	}
	var lastTerminalErr error
	var lastTerminalEndpoint string
	var lastNonTerminalErr error
	var lastNonTerminalEndpoint string
	terminalCount := 0
	for _, ep := range order {
		res, err := FetchWithDecryptURL(ctx, ep, decryptURL, ifNoneMatchRevision)
		if err == nil {
			return res, ep, nil
		}
		if wf, ok := AsWGFeedError(err); ok && !wf.Retriable {
			terminalCount++
			lastTerminalErr = err
			lastTerminalEndpoint = ep
			continue
		}
		lastNonTerminalErr = err
		lastNonTerminalEndpoint = ep
	}
	if terminalCount == len(order) && lastTerminalErr != nil {
		return FetchResult{}, lastTerminalEndpoint, lastTerminalErr
	}
	if lastNonTerminalErr != nil {
		return FetchResult{}, lastNonTerminalEndpoint, lastNonTerminalErr
	}
	if lastTerminalErr != nil {
		// Should be rare (e.g., order had endpoints but all were terminal and already handled)
		return FetchResult{}, lastTerminalEndpoint, lastTerminalErr
	}
	return FetchResult{}, "", fmt.Errorf("all endpoints failed")
}

// StreamSSEAnyEndpoints attempts to open an SSE stream from endpoints[] in randomized order.
// If a stream fails for a retriable reason, it tries the next endpoint.
func StreamSSEAnyEndpoints(ctx context.Context, endpoints []string, onEvent func(endpoint string, data []byte) error) error {
	order := randomizedCopy(endpoints)
	if len(order) == 0 {
		return fmt.Errorf("no endpoints")
	}
	var lastTerminalErr error
	var lastNonTerminalErr error
	terminalCount := 0
	for _, ep := range order {
		err := StreamSSE(ctx, ep, func(data []byte) error {
			return onEvent(ep, data)
		})
		if err == nil {
			return nil
		}
		if err == ErrStreamNotSupported {
			return err
		}
		if wf, ok := AsWGFeedError(err); ok && !wf.Retriable {
			terminalCount++
			lastTerminalErr = err
			continue
		}
		lastNonTerminalErr = err
	}
	if terminalCount == len(order) && lastTerminalErr != nil {
		return lastTerminalErr
	}
	if lastNonTerminalErr != nil {
		return lastNonTerminalErr
	}
	if lastTerminalErr != nil {
		return lastTerminalErr
	}
	return fmt.Errorf("all endpoints failed")
}
