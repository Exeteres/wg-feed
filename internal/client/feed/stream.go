package feed

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var ErrStreamNotSupported = errors.New("stream not supported")

// StreamSSE opens an SSE stream for the given URL.
// It returns ErrStreamNotSupported if the server responds with a non-SSE content-type.
// Each event is expected to contain exactly one "data: " line with the full JSON payload.
func StreamSSE(ctx context.Context, url string, onEvent func(data []byte) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// Draft-00: clients send exactly one Accept media type.
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
			if er, ok := tryDecodeErrorResponse(b); ok {
				return &WGFeedError{Status: resp.StatusCode, Message: er.Message, Retriable: er.Retriable}
			}
		}
		return fmt.Errorf("GET %s: unexpected status %d: %s", RedactURL(url), resp.StatusCode, string(b))
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(ct, "text/event-stream") {
		return ErrStreamNotSupported
	}

	r := bufio.NewReader(resp.Body)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, err := readOneSSEDataEvent(r)
		if err != nil {
			return err
		}
		if err := onEvent(data); err != nil {
			return err
		}
	}
}

func readOneSSEDataEvent(r *bufio.Reader) ([]byte, error) {
	var eventType string
	var data []byte
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			if eventType == "feed" && len(data) != 0 {
				return data, nil
			}
			// Reset state for next event.
			eventType = ""
			data = nil
			continue
		}
		if strings.HasPrefix(trimmed, "event: ") {
			eventType = strings.TrimSpace(strings.TrimPrefix(trimmed, "event: "))
			continue
		}
		if strings.HasPrefix(trimmed, "data: ") {
			data = []byte(strings.TrimPrefix(trimmed, "data: "))
		}
	}
}
