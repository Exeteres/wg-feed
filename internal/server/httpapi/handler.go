package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/exeteres/wg-feed/internal/model"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type getter interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
}

type watcher interface {
	Watch(ctx context.Context, key string) clientv3.WatchChan
}

type Handler struct {
	store  getter
	logger *log.Logger
}

func NewHandler(store getter, logger *log.Logger) *Handler {
	return &Handler{store: store, logger: logger}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", false)
		return
	}

	mode := negotiateResponseMode(r)

	feedPath := strings.TrimPrefix(r.URL.Path, "/")
	feedPath = strings.Trim(feedPath, "/")
	if feedPath == "" {
		h.writeError(w, http.StatusNotFound, "feed not found", false)
		return
	}

	key := "wg-feed/feeds/" + feedPath

	if mode == responseModeSSE {
		if r.Method != http.MethodGet {
			h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", false)
			return
		}
		h.serveSSE(w, r, feedPath, key)
		return
	}
	if mode == responseModeOther {
		h.writeError(w, http.StatusNotAcceptable, "unsupported Accept value", false)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	body, ok, err := h.store.Get(ctx, key)
	if err != nil {
		h.logger.Printf("etcd get failed feedPath=%q key=%q err=%v", feedPath, key, err)
		h.writeError(w, http.StatusInternalServerError, "internal error", true)
		return
	}
	if !ok {
		h.writeError(w, http.StatusNotFound, "feed not found", false)
		return
	}

	entry, err := decodeAndValidateEntry(body)
	if err != nil {
		h.logger.Printf("feed entry invalid feedPath=%q key=%q err=%v", feedPath, key, err)
		h.writeError(w, http.StatusInternalServerError, "invalid feed entry", true)
		return
	}

	respBody, etag, err := entryToSuccessResponseJSON(entry)
	if err != nil {
		h.logger.Printf("feed entry invalid feedPath=%q key=%q err=%v", feedPath, key, err)
		h.writeError(w, http.StatusInternalServerError, "invalid feed entry", true)
		return
	}

	if strings.TrimSpace(etag) != "" {
		w.Header().Set("ETag", etag)
		if ifNoneMatchMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(respBody)
}

const (
	acceptJSON = "application/json"
	acceptSSE  = "text/event-stream"
)

type responseMode int

const (
	responseModeJSON responseMode = iota
	responseModeSSE
	responseModeOther
)

func negotiateResponseMode(r *http.Request) responseMode {
	vals := r.Header.Values("Accept")
	// Missing/empty Accept is not treated as JSON.
	if len(vals) == 0 {
		return responseModeOther
	}

	wantsJSON := false
	wantsSSE := false

	for _, headerVal := range vals {
		headerVal = strings.TrimSpace(headerVal)
		if headerVal == "" {
			continue
		}
		for _, part := range strings.Split(headerVal, ",") {
			mediaRange := strings.TrimSpace(part)
			if mediaRange == "" {
				continue
			}
			// Valid (non-empty) media range seen.
			// Ignore any media type parameters (e.g., q=).
			if semi := strings.Index(mediaRange, ";"); semi >= 0 {
				mediaRange = strings.TrimSpace(mediaRange[:semi])
			}
			mediaRange = strings.ToLower(mediaRange)

			if mediaRange == acceptSSE {
				wantsSSE = true
				continue
			}
			if mediaRange == acceptJSON {
				wantsJSON = true
				continue
			}
		}
	}

	// SSE takes precedence when explicitly requested.
	if wantsSSE {
		return responseModeSSE
	}
	if wantsJSON {
		return responseModeJSON
	}
	return responseModeOther
}

func decodeAndValidateEntry(body []byte) (model.FeedEntry, error) {
	var entry model.FeedEntry
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&entry); err != nil {
		return model.FeedEntry{}, fmt.Errorf("decode entry: %w", err)
	}
	if err := entry.Validate(); err != nil {
		return model.FeedEntry{}, fmt.Errorf("validate entry: %w", err)
	}
	return entry, nil
}

func entryToSuccessResponseJSON(entry model.FeedEntry) ([]byte, string, error) {
	if entry.Encrypted {
		sr := model.SuccessResponse{
			Version:       "wg-feed-00",
			Success:       true,
			Revision:      entry.Revision,
			TTLSeconds:    entry.TTLSeconds,
			SupportsSSE:   true,
			Encrypted:     true,
			EncryptedData: entry.EncryptedData,
		}
		if err := sr.Validate(); err != nil {
			return nil, "", err
		}
		b, err := json.Marshal(sr)
		return b, formatETagHeaderValue(entry.Revision), err
	}
	sr := model.SuccessResponse{
		Version:     "wg-feed-00",
		Success:     true,
		Revision:    entry.Revision,
		TTLSeconds:  entry.TTLSeconds,
		SupportsSSE: true,
		Data:        entry.Data,
	}
	if err := sr.Validate(); err != nil {
		return nil, "", err
	}
	etag := formatETagHeaderValue(entry.Revision)
	b, err := json.Marshal(sr)
	return b, etag, err
}

func formatETagHeaderValue(revision string) string {
	rev := strings.TrimSpace(revision)
	if rev == "" {
		return ""
	}
	// Draft-00: ETag MUST be a strong entity-tag with payload equal to revision.
	// That is, the header value must be quoted: "<revision>".
	return fmt.Sprintf("%q", rev)
}

func normalizeETag(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "w/") {
		s = strings.TrimSpace(s[2:])
	}
	s = strings.TrimSpace(s)
	if len(s) >= 2 && strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "\""), "\"")
	}
	return s
}

func ifNoneMatchMatches(headerValue string, revision string) bool {
	if strings.TrimSpace(headerValue) == "" {
		return false
	}
	// If-None-Match can be a list.
	for _, part := range strings.Split(headerValue, ",") {
		if normalizeETag(part) == normalizeETag(revision) {
			return true
		}
	}
	return false
}

func (h *Handler) serveSSE(w http.ResponseWriter, r *http.Request, feedPath, key string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, http.StatusNotImplemented, "streaming not supported", true)
		return
	}

	getCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	body, ok2, err := h.store.Get(getCtx, key)
	if err != nil {
		h.logger.Printf("etcd get failed feedPath=%q key=%q err=%v", feedPath, key, err)
		h.writeError(w, http.StatusInternalServerError, "internal error", true)
		return
	}
	if !ok2 {
		h.writeError(w, http.StatusNotFound, "feed not found", false)
		return
	}
	entry, err := decodeAndValidateEntry(body)
	if err != nil {
		h.logger.Printf("feed entry invalid feedPath=%q key=%q err=%v", feedPath, key, err)
		h.writeError(w, http.StatusInternalServerError, "invalid feed entry", true)
		return
	}

	respBody, _, err := entryToSuccessResponseJSON(entry)
	if err != nil {
		h.logger.Printf("feed entry invalid feedPath=%q key=%q err=%v", feedPath, key, err)
		h.writeError(w, http.StatusInternalServerError, "invalid feed entry", true)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	writeEvent := func(b []byte) error {
		if _, err := io.WriteString(w, "event: feed\n"); err != nil {
			return err
		}
		// Single data: line containing full JSON success response.
		if _, err := io.WriteString(w, "data: "); err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n\n"); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if err := writeEvent(respBody); err != nil {
		return
	}

	ws, ok := h.store.(watcher)
	if !ok {
		// Store doesn't support watch; just serve the initial event.
		return
	}

	watchCh := ws.Watch(r.Context(), key)
	for {
		select {
		case <-r.Context().Done():
			return
		case wr, ok := <-watchCh:
			if !ok {
				return
			}
			if wr.Err() != nil {
				h.logger.Printf("etcd watch failed feedPath=%q key=%q err=%v", feedPath, key, wr.Err())
				return
			}
			for _, ev := range wr.Events {
				if ev.Type != mvccpb.PUT || ev.Kv == nil {
					continue
				}
				b := ev.Kv.Value
				entry, err := decodeAndValidateEntry(b)
				if err != nil {
					h.logger.Printf("feed entry invalid feedPath=%q key=%q err=%v", feedPath, key, err)
					continue
				}
				respBody, _, err := entryToSuccessResponseJSON(entry)
				if err != nil {
					h.logger.Printf("feed entry invalid feedPath=%q key=%q err=%v", feedPath, key, err)
					continue
				}
				if err := writeEvent(respBody); err != nil {
					return
				}
			}
		}
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string, retriable bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(model.ErrorResponse{
		Version:   "wg-feed-00",
		Success:   false,
		Message:   message,
		Retriable: retriable,
	})
}
