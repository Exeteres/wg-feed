package feed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/exeteres/wg-feed/internal/model"
)

type WGFeedError struct {
	Status    int
	Message   string
	Retriable bool
}

func (e *WGFeedError) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = "wg-feed error"
	}
	return fmt.Sprintf("wg-feed error: status=%d message=%q retriable=%v", e.Status, msg, e.Retriable)
}

func AsWGFeedError(err error) (*WGFeedError, bool) {
	var e *WGFeedError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

func FetchAndValidate(ctx context.Context, url string) (model.FeedDocument, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return model.FeedDocument{}, nil, err
	}
	// Draft-00: clients send exactly one Accept media type.
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.FeedDocument{}, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.FeedDocument{}, nil, err
	}

	if resp.StatusCode != http.StatusOK {
		// Non-200 responses may include a wg-feed JSON error envelope, but clients must not assume it.
		if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
			if er, ok := tryDecodeErrorResponse(body); ok {
				return model.FeedDocument{}, nil, &WGFeedError{Status: resp.StatusCode, Message: er.Message, Retriable: er.Retriable}
			}
		}
		snippet := string(body)
		if len(snippet) > 4096 {
			snippet = snippet[:4096]
		}
		return model.FeedDocument{}, nil, fmt.Errorf("GET %s: unexpected status %d: %s", RedactURL(url), resp.StatusCode, snippet)
	}

	sr, err := decodeSuccessResponse(body)
	if err != nil {
		return model.FeedDocument{}, nil, err
	}
	if sr.Encrypted {
		doc, err := DecryptFeedDocumentForSetupURL(url, sr.EncryptedData)
		if err != nil {
			return model.FeedDocument{}, nil, err
		}
		return doc, body, nil
	}
	if sr.Data == nil {
		return model.FeedDocument{}, nil, fmt.Errorf("validate response: data is required when encrypted=false")
	}
	return *sr.Data, body, nil
}

func decodeSuccessResponse(body []byte) (model.SuccessResponse, error) {
	var sr model.SuccessResponse
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&sr); err != nil {
		return model.SuccessResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if err := sr.Validate(); err != nil {
		return model.SuccessResponse{}, fmt.Errorf("validate response: %w", err)
	}
	return sr, nil
}

func tryDecodeErrorResponse(body []byte) (model.ErrorResponse, bool) {
	var er model.ErrorResponse
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&er); err != nil {
		return model.ErrorResponse{}, false
	}
	if err := er.Validate(); err != nil {
		return model.ErrorResponse{}, false
	}
	return er, true
}
