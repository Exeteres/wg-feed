package feed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/exeteres/wg-feed/internal/model"
)

type FetchResult struct {
	NotModified bool
	Revision    string
	TTLSeconds  int
	SupportsSSE bool
	Encrypted   bool
	// EncryptedData is the armored ciphertext from the server when Encrypted=true.
	// It can be reused directly for offline cache reconciliation.
	EncryptedData string
	Feed          model.FeedDocument
	Body          []byte
}

func fetchSuccessResponse(ctx context.Context, url string, ifNoneMatchRevision string) (model.SuccessResponse, []byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return model.SuccessResponse{}, nil, false, err
	}
	// Draft-00: clients send exactly one Accept media type.
	req.Header.Set("Accept", "application/json")
	if tag := formatIfNoneMatchValue(ifNoneMatchRevision); tag != "" {
		req.Header.Set("If-None-Match", tag)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.SuccessResponse{}, nil, false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return model.SuccessResponse{}, nil, true, nil
	case http.StatusOK:
		// continue
	default:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
			if er, ok := tryDecodeErrorResponse(b); ok {
				return model.SuccessResponse{}, nil, false, &WGFeedError{Status: resp.StatusCode, Message: er.Message, Retriable: er.Retriable}
			}
		}
		return model.SuccessResponse{}, nil, false, fmt.Errorf("GET %s: unexpected status %d: %s", RedactURL(url), resp.StatusCode, string(b))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.SuccessResponse{}, nil, false, err
	}

	sr, err := decodeSuccessResponse(body)
	if err != nil {
		return model.SuccessResponse{}, nil, false, err
	}
	return sr, body, false, nil
}

func FetchConditional(ctx context.Context, url string, ifNoneMatchRevision string) (FetchResult, error) {
	sr, body, notModified, err := fetchSuccessResponse(ctx, url, ifNoneMatchRevision)
	if err != nil {
		return FetchResult{}, err
	}
	if notModified {
		return FetchResult{NotModified: true, Revision: strings.TrimSpace(ifNoneMatchRevision)}, nil
	}

	res := FetchResult{}
	if sr.Encrypted {
		doc, err := DecryptFeedDocumentForSetupURL(url, sr.EncryptedData)
		if err != nil {
			return FetchResult{}, err
		}
		res.Revision = strings.TrimSpace(sr.Revision)
		res.TTLSeconds = sr.TTLSeconds
		res.SupportsSSE = sr.SupportsSSE
		res.Encrypted = true
		res.EncryptedData = sr.EncryptedData
		res.Feed = doc
		res.Body = body
		return res, nil
	}
	if sr.Data == nil {
		return FetchResult{}, fmt.Errorf("validate response: data is required when encrypted=false")
	}
	res.Revision = strings.TrimSpace(sr.Revision)
	res.TTLSeconds = sr.TTLSeconds
	res.SupportsSSE = sr.SupportsSSE
	res.Encrypted = false
	res.EncryptedData = ""
	res.Feed = *sr.Data
	res.Body = body
	return res, nil
}

func formatIfNoneMatchValue(revision string) string {
	rev := strings.TrimSpace(revision)
	if rev == "" {
		return ""
	}
	// Draft-00: servers set ETag to a quoted entity-tag whose payload equals revision.
	return fmt.Sprintf("%q", rev)
}
