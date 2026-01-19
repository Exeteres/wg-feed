package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/exeteres/wg-feed/internal/client"
	"github.com/exeteres/wg-feed/internal/client/backend"
	"github.com/exeteres/wg-feed/internal/client/config"
	"github.com/exeteres/wg-feed/internal/client/feed"
	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/model"
)

const (
	defaultTickOnFailure = 1 * time.Minute
	minTick              = 5 * time.Second
	defaultReconcileTick = 1 * time.Minute
	streamRetryDelay     = 2 * time.Second
)

func Run(ctx context.Context, cfg config.Config, logger *log.Logger) error {
	b, err := backend.New(cfg, logger)
	if err != nil {
		return err
	}

	d := &daemon{
		cfg:    cfg,
		b:      b,
		logger: logger,
	}

	errCh := make(chan error, len(cfg.SetupURLs))
	for _, url := range cfg.SetupURLs {
		setupURL := url
		go func() {
			err := d.runFeed(ctx, setupURL)
			if err != nil {
				logger.Printf("feed loop exited feed=%q err=%v", feed.RedactURL(setupURL), err)
			}
			errCh <- err
		}()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

type daemon struct {
	cfg    config.Config
	b      backend.Backend
	logger *log.Logger

	mu sync.Mutex

	claimedMu sync.Mutex
	claimed   map[string]string // feedID -> setupURL
}

func (d *daemon) runFeed(ctx context.Context, setupURL string) error {
	setupURL = strings.TrimSpace(setupURL)
	var feedID string
	var endpoints []string
	var lastRevision string
	var lastTTL *int
	var nextCacheReconcile time.Time

	// Best-effort: resolve feedID + endpoints from cached encrypted_data before any network bootstrap.
	resolvedID, resolvedEndpoints, err := d.resolveFromStateCache(setupURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(resolvedID) != "" {
		feedID = strings.TrimSpace(resolvedID)
		if !d.claimFeedID(feedID, setupURL) {
			return nil
		}
	}
	if len(resolvedEndpoints) != 0 {
		endpoints = resolvedEndpoints
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// If we don't yet know endpoints, bootstrap once using the setup URL.
		if len(endpoints) == 0 {
			res, _, err := feed.FetchAnyEndpoints(ctx, []string{setupURL}, setupURL, "")
			if err != nil {
				if wf, ok := feed.AsWGFeedError(err); ok && !wf.Retriable {
					return err
				}
				d.logger.Printf("bootstrap fetch failed feed=%q err=%v", feed.RedactURL(setupURL), err)
				if err := d.maybeReconcileFromCache(ctx, setupURL, feedID, nextCacheReconcile); err == nil {
					nextCacheReconcile = time.Now().Add(defaultReconcileTick)
				}
				sleep(ctx, defaultTickOnFailure)
				continue
			}
			if feedID == "" {
				feedID = strings.TrimSpace(res.Feed.ID)
				if feedID == "" {
					return fmt.Errorf("missing feed id")
				}
				if !d.claimFeedID(feedID, setupURL) {
					return nil
				}
			}
			endpoints = res.Feed.Endpoints
			cached := ""
			if res.Encrypted {
				cached = res.EncryptedData
			}
			if err := d.applyRemoteUpdate(ctx, setupURL, setupURL, res.Feed, res.Revision, res.TTLSeconds, cached); err != nil {
				return err
			}
			lastRevision = strings.TrimSpace(res.Revision)
			v := res.TTLSeconds
			lastTTL = &v
			continue
		}

		// Prefer SSE when available.
		err := feed.StreamSSEAnyEndpoints(ctx, endpoints, func(endpoint string, data []byte) error {
			doc, rev, ttl, encryptedData, err := decodeAndValidateSuccess(setupURL, data)
			if err != nil {
				if wf, ok := feed.AsWGFeedError(err); ok && !wf.Retriable {
					return err
				}
				d.logger.Printf("stream event invalid feed=%q err=%v", feed.RedactURL(endpoint), err)
				return nil
			}
			lastTTL = &ttl
			lastRevision = strings.TrimSpace(rev)
			if feedID == "" {
				feedID = strings.TrimSpace(doc.ID)
				if feedID == "" {
					return fmt.Errorf("missing feed id")
				}
				if !d.claimFeedID(feedID, setupURL) {
					return nil
				}
			}
			endpoints = doc.Endpoints
			if err := d.applyRemoteUpdate(ctx, endpoint, setupURL, doc, rev, ttl, encryptedData); err != nil {
				return err
			}
			return nil
		})

		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, feed.ErrStreamNotSupported) {
			res, _, fetchErr := feed.FetchAnyEndpoints(ctx, endpoints, setupURL, "")
			if fetchErr == nil && res.SupportsSSE {
				d.logger.Printf("stream not supported for %s but supports_sse=true; retrying stream", feed.RedactURL(setupURL))
				continue
			}
			d.logger.Printf("stream not supported for %s; using polling", feed.RedactURL(setupURL))
			return d.pollLoop(ctx, setupURL, &feedID, &endpoints, &lastRevision, &lastTTL, &nextCacheReconcile)
		}
		if wf, ok := feed.AsWGFeedError(err); ok && !wf.Retriable {
			d.logger.Printf("wg-feed error (non-retriable) feed=%q message=%q; stopping automatic reconnect", feed.RedactURL(setupURL), wf.Message)
			<-ctx.Done()
			return ctx.Err()
		}

		// Any other error: retry SSE after delay.
		d.logger.Printf("stream error for %s; retrying: %v", feed.RedactURL(setupURL), err)
		if err := d.maybeReconcileFromCache(ctx, setupURL, feedID, nextCacheReconcile); err == nil {
			nextCacheReconcile = time.Now().Add(defaultReconcileTick)
		}
		sleep(ctx, streamRetryDelay)
	}
}

func (d *daemon) resolveFromStateCache(setupURL string) (string, []string, error) {
	setupURL = strings.TrimSpace(setupURL)
	var feedID string
	var endpoints []string
	err := d.withStateSave(func(st *state.State) error {
		key, err := st.SetupURLKey(setupURL)
		if err != nil {
			return err
		}
		feedID = strings.TrimSpace(st.SetupURLMap[key])
		if feedID == "" {
			return nil
		}
		fs, ok := st.Feeds[feedID]
		if !ok {
			return nil
		}
		if strings.TrimSpace(fs.CachedEncryptedData) == "" {
			return nil
		}
		doc, err := feed.DecryptFeedDocumentForSetupURL(setupURL, fs.CachedEncryptedData)
		if err != nil {
			return err
		}
		endpoints = doc.Endpoints
		// If the cached doc ID doesn't match, prefer the cached doc and update the mapping.
		cachedID := strings.TrimSpace(doc.ID)
		if cachedID != "" && cachedID != feedID {
			st.SetupURLMap[key] = cachedID
			feedID = cachedID
		}
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return feedID, endpoints, nil
}

func (d *daemon) claimFeedID(feedID, setupURL string) bool {
	d.claimedMu.Lock()
	defer d.claimedMu.Unlock()
	if d.claimed == nil {
		d.claimed = map[string]string{}
	}
	if existing, ok := d.claimed[feedID]; ok {
		if existing != setupURL {
			d.logger.Printf("duplicate setup url ignored: feed_id=%q url=%q already_claimed_by=%q", feedID, feed.RedactURL(setupURL), feed.RedactURL(existing))
		}
		return false
	}
	d.claimed[feedID] = setupURL
	return true
}

func (d *daemon) withStateSave(fn func(st *state.State) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	st, err := state.Load(d.cfg.StatePath)
	if err != nil {
		return err
	}
	errFn := fn(&st)
	errSave := state.SaveAtomic(d.cfg.StatePath, st)
	if errSave != nil {
		return errSave
	}
	return errFn
}

func (d *daemon) pollLoop(ctx context.Context, setupURL string, feedID *string, endpoints *[]string, lastRevision *string, lastTTL **int, nextCacheReconcile *time.Time) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		res, usedEndpoint, err := feed.FetchAnyEndpoints(ctx, *endpoints, setupURL, strings.TrimSpace(*lastRevision))
		if err != nil {
			if wf, ok := feed.AsWGFeedError(err); ok && !wf.Retriable {
				d.logger.Printf("wg-feed error (non-retriable) feed=%q message=%q; stopping automatic polling", feed.RedactURL(setupURL), wf.Message)
				<-ctx.Done()
				return ctx.Err()
			}
			d.logger.Printf("poll fetch failed feed=%q err=%v", feed.RedactURL(setupURL), err)
			if err := d.maybeReconcileFromCache(ctx, setupURL, strings.TrimSpace(*feedID), *nextCacheReconcile); err == nil {
				*nextCacheReconcile = time.Now().Add(defaultReconcileTick)
			}
			sleep(ctx, defaultTickOnFailure)
			continue
		}
		if res.NotModified {
			// Successful sync: no document changes.
			s := defaultTickOnFailure
			if *lastTTL != nil && **lastTTL > 0 {
				s = time.Duration(**lastTTL) * time.Second
			}
			if s < minTick {
				s = minTick
			}
			sleep(ctx, s)
			continue
		}
		*lastRevision = strings.TrimSpace(res.Revision)
		if *feedID == "" {
			*feedID = strings.TrimSpace(res.Feed.ID)
			if *feedID == "" {
				return fmt.Errorf("missing feed id")
			}
			if !d.claimFeedID(*feedID, setupURL) {
				return nil
			}
		}
		*endpoints = res.Feed.Endpoints
		v := res.TTLSeconds
		*lastTTL = &v

		cached := ""
		if res.Encrypted {
			cached = res.EncryptedData
		}
		if err := d.applyRemoteUpdate(ctx, usedEndpoint, setupURL, res.Feed, res.Revision, res.TTLSeconds, cached); err != nil {
			if wf, ok := feed.AsWGFeedError(err); ok && !wf.Retriable {
				d.logger.Printf("wg-feed error (non-retriable) feed=%q message=%q; stopping automatic polling", feed.RedactURL(setupURL), wf.Message)
				<-ctx.Done()
				return ctx.Err()
			}
			d.logger.Printf("reconcile failed feed=%q err=%v", feed.RedactURL(setupURL), err)
		}

		s := defaultTickOnFailure
		if *lastTTL != nil && **lastTTL > 0 {
			s = time.Duration(**lastTTL) * time.Second
		}
		if s < minTick {
			s = minTick
		}
		sleep(ctx, s)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	select {
	case <-ctx.Done():
		t.Stop()
	case <-t.C:
	}
}

func decodeAndValidateSuccess(setupURL string, body []byte) (model.FeedDocument, string, int, string, error) {
	var sr model.SuccessResponse
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&sr); err != nil {
		return model.FeedDocument{}, "", 0, "", err
	}
	if err := sr.Validate(); err != nil {
		return model.FeedDocument{}, "", 0, "", err
	}
	if sr.Encrypted {
		doc, err := feed.DecryptFeedDocumentForSetupURL(setupURL, sr.EncryptedData)
		if err != nil {
			return model.FeedDocument{}, "", 0, "", err
		}
		rev := strings.TrimSpace(sr.Revision)
		return doc, rev, sr.TTLSeconds, sr.EncryptedData, nil
	}
	if sr.Data == nil {
		return model.FeedDocument{}, "", 0, "", fmt.Errorf("missing data")
	}
	rev := strings.TrimSpace(sr.Revision)
	return *sr.Data, rev, sr.TTLSeconds, "", nil
}

func (d *daemon) applyRemoteUpdate(ctx context.Context, requestURL string, setupURL string, doc model.FeedDocument, revision string, ttl int, cachedEncryptedData string) error {
	feedID := strings.TrimSpace(doc.ID)
	if feedID == "" {
		return fmt.Errorf("missing feed id")
	}
	if msg := strings.TrimSpace(doc.Warning); msg != "" {
		d.logger.Printf("feed warning: feed=%q message=%q", feed.RedactURL(setupURL), msg)
	}
	return d.withStateSave(func(st *state.State) error {
		key, err := st.SetupURLKey(setupURL)
		if err != nil {
			return err
		}
		st.SetupURLMap[key] = feedID

		fs := st.Feeds[feedID]
		if fs.Tunnels == nil {
			fs.Tunnels = map[string]state.TunnelState{}
		}
		v := ttl
		fs.TTLSeconds = &v
		fs.CachedEncryptedData = strings.TrimSpace(cachedEncryptedData)
		st.Feeds[feedID] = fs

		// Spec: only reconcile when revision changed since last successfully reconciled.
		if strings.TrimSpace(revision) != "" && strings.TrimSpace(fs.LastReconciledRevision) == strings.TrimSpace(revision) {
			return nil
		}

		if err := client.ApplyFeed(ctx, d.cfg, d.b, st, requestURL, doc, d.logger); err != nil {
			return err
		}
		fs = st.Feeds[feedID]
		fs.LastReconciledRevision = strings.TrimSpace(revision)
		st.Feeds[feedID] = fs
		return nil
	})
}

func (d *daemon) maybeReconcileFromCache(ctx context.Context, setupURL string, feedID string, notBefore time.Time) error {
	if !notBefore.IsZero() && time.Now().Before(notBefore) {
		return fmt.Errorf("cache reconcile throttled")
	}
	feedID = strings.TrimSpace(feedID)
	if feedID == "" {
		return fmt.Errorf("no cached config")
	}
	return d.withStateSave(func(st *state.State) error {
		fs, ok := st.Feeds[feedID]
		if !ok {
			return fmt.Errorf("no cached config")
		}
		if strings.TrimSpace(fs.CachedEncryptedData) == "" {
			return fmt.Errorf("no cached config")
		}
		doc, err := feed.DecryptFeedDocumentForSetupURL(setupURL, fs.CachedEncryptedData)
		if err != nil {
			return err
		}
		// Forced reconciliation while offline.
		return client.ApplyFeed(ctx, d.cfg, d.b, st, setupURL, doc, d.logger)
	})
}
