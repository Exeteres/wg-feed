package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
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

var transportURLRe = regexp.MustCompile(`https?://[^\s"']+`)

type daemonFeed struct {
	label      string
	cfg        config.FeedConfig
	backends   map[string]backend.Backend
	decryptURL string
}

type daemon struct {
	cfg    config.Config
	logger *slog.Logger
	feeds  map[string]daemonFeed

	mu sync.Mutex

	claimedMu sync.Mutex
	claimed   map[string]string // feedID -> configured feed label
}

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	d := &daemon{
		cfg:    cfg,
		logger: logger,
		feeds:  map[string]daemonFeed{},
	}

	labels := make([]string, 0, len(cfg.Feeds))
	for label := range cfg.Feeds {
		labels = append(labels, label)
	}
	sort.Strings(labels)

	for _, label := range labels {
		feedCfg := cfg.Feeds[label]
		if !feedCfg.Sync.Enabled {
			continue
		}

		backendSet := map[string]backend.Backend{}
		for backendLabel, backendCfg := range feedCfg.Backends {
			b, err := backend.NewForType(backendCfg.Type, logger)
			if err != nil {
				return fmt.Errorf("feed %s backend %s: %w", label, backendLabel, err)
			}
			backendSet[backendLabel] = b
		}

		d.feeds[label] = daemonFeed{
			label:      label,
			cfg:        feedCfg,
			backends:   backendSet,
			decryptURL: feedCfg.DecryptURL(),
		}
	}

	if len(d.feeds) == 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	errCh := make(chan error, len(d.feeds))
	for _, feedRuntime := range d.feeds {
		rf := feedRuntime
		go func() {
			err := d.runFeed(ctx, rf)
			if err != nil {
				logger.Error("feed loop exited", "feed_label", rf.label, "err", redactTransportError(err))
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

func (d *daemon) runFeed(ctx context.Context, rf daemonFeed) error {
	feedLabel := rf.label
	var feedID string
	endpoints := append([]string(nil), rf.cfg.Sync.Endpoints...)
	var lastRevision string
	var lastTTL *int
	var nextCacheReconcile time.Time

	resolvedID, resolvedEndpoints, err := d.resolveFromStateCache(feedLabel, rf.decryptURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(resolvedID) != "" {
		feedID = strings.TrimSpace(resolvedID)
		if !d.claimFeedID(feedID, feedLabel) {
			return nil
		}
	}
	if len(resolvedEndpoints) != 0 {
		endpoints = resolvedEndpoints
	}

	if err := d.reconcileFromCacheOnStart(ctx, rf, feedID); err == nil {
		nextCacheReconcile = time.Now().Add(defaultReconcileTick)
	}

	if strings.EqualFold(string(rf.cfg.Sync.Mode), string(config.SyncModePolling)) {
		d.logger.Info("feed loop started", "feed_label", feedLabel, "mode", "polling", "endpoints", len(endpoints))
		return d.pollLoop(ctx, rf, &feedID, &endpoints, &lastRevision, &lastTTL, &nextCacheReconcile)
	}
	d.logger.Info("feed loop started", "feed_label", feedLabel, "mode", "sse", "endpoints", len(endpoints))

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if strings.TrimSpace(feedID) != "" {
			_ = d.withStateRead(func(st state.State) error {
				endpoints = st.OrderEndpoints(feedLabel, endpoints)
				return nil
			})
		}

		err := feed.StreamSSEAnyEndpoints(ctx, endpoints, func(endpoint string, data []byte) error {
			doc, rev, ttl, encryptedData, err := decodeAndValidateSuccess(rf.decryptURL, data)
			if err != nil {
				if wf, ok := feed.AsWGFeedError(err); ok && !wf.Retriable {
					return err
				}
				d.logger.Warn("stream event invalid", "feed_label", feedLabel, "endpoint", feed.RedactURL(endpoint), "err", redactTransportError(err))
				return nil
			}

			lastTTL = &ttl
			lastRevision = strings.TrimSpace(rev)
			if feedID == "" {
				feedID = strings.TrimSpace(doc.ID)
				if feedID == "" {
					return fmt.Errorf("missing feed id")
				}
				if !d.claimFeedID(feedID, feedLabel) {
					return nil
				}
			}
			endpoints = doc.Endpoints
			d.logger.Debug("sse feed event received", "feed_label", feedLabel, "endpoint", feed.RedactURL(endpoint), "revision", strings.TrimSpace(rev), "ttl_seconds", ttl, "encrypted", strings.TrimSpace(encryptedData) != "")
			if err := d.applyRemoteUpdate(ctx, rf, endpoint, doc, rev, ttl, encryptedData); err != nil {
				return err
			}
			d.logger.Debug("sse feed event applied", "feed_label", feedLabel, "revision", strings.TrimSpace(rev), "feed_id", strings.TrimSpace(doc.ID), "tunnels", len(doc.Tunnels))
			return nil
		})

		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, feed.ErrStreamNotSupported) {
			res, _, fetchErr := feed.FetchAnyEndpoints(ctx, endpoints, rf.decryptURL, "")
			if fetchErr == nil && res.SupportsSSE {
				d.logger.Info("stream not supported but supports_sse=true; retrying", "feed_label", feedLabel)
				continue
			}
			d.logger.Info("stream not supported; switching to polling", "feed_label", feedLabel)
			return d.pollLoop(ctx, rf, &feedID, &endpoints, &lastRevision, &lastTTL, &nextCacheReconcile)
		}
		if wf, ok := feed.AsWGFeedError(err); ok && !wf.Retriable {
			d.logger.Error("wg-feed non-retriable error; stopping reconnect", "feed_label", feedLabel, "message", wf.Message)
			<-ctx.Done()
			return ctx.Err()
		}

		d.logger.Warn("stream error; retrying", "feed_label", feedLabel, "err", redactTransportError(err))
		if err := d.maybeReconcileFromCache(ctx, rf, feedID, nextCacheReconcile); err == nil {
			nextCacheReconcile = time.Now().Add(defaultReconcileTick)
		}
		d.logger.Debug("stream retry sleep", "feed_label", feedLabel, "sleep", streamRetryDelay.String())
		sleep(ctx, streamRetryDelay)
	}
}

func (d *daemon) resolveFromStateCache(feedLabel string, decryptURL string) (string, []string, error) {
	var feedID string
	var endpoints []string
	err := d.withStateSave(func(st *state.State) error {
		fs, ok := st.Feeds[feedLabel]
		if !ok {
			return nil
		}
		feedID = strings.TrimSpace(fs.ID)
		if feedID == "" || strings.TrimSpace(fs.CachedEncryptedData) == "" {
			return nil
		}
		doc, err := feed.DecryptFeedDocumentForURL(decryptURL, fs.CachedEncryptedData)
		if err != nil {
			return err
		}
		endpoints = st.OrderEndpoints(feedLabel, doc.Endpoints)
		cachedID := strings.TrimSpace(doc.ID)
		if cachedID != "" && cachedID != feedID {
			fs.ID = cachedID
			st.Feeds[feedLabel] = fs
			feedID = cachedID
			endpoints = st.OrderEndpoints(feedLabel, doc.Endpoints)
		}
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(feedID), endpoints, nil
}

func (d *daemon) claimFeedID(feedID, feedLabel string) bool {
	d.claimedMu.Lock()
	defer d.claimedMu.Unlock()
	if d.claimed == nil {
		d.claimed = map[string]string{}
	}
	if existing, ok := d.claimed[feedID]; ok {
		if existing != feedLabel {
			d.logger.Info("duplicate feed ignored", "feed_id", feedID, "feed_label", feedLabel, "already_claimed_by", existing)
		}
		return false
	}
	d.claimed[feedID] = feedLabel
	return true
}

func (d *daemon) reconcileFromCacheOnStart(ctx context.Context, rf daemonFeed, feedID string) error {
	feedID = strings.TrimSpace(feedID)
	if feedID == "" {
		return fmt.Errorf("no cached config")
	}
	if err := d.maybeReconcileFromCache(ctx, rf, feedID, time.Time{}); err != nil {
		d.logger.Debug("startup cache reconcile skipped", "feed_label", rf.label, "err", redactTransportError(err))
		return err
	}
	d.logger.Debug("startup cache reconcile applied", "feed_label", rf.label)
	return nil
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

func (d *daemon) withStateRead(fn func(st state.State) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	st, err := state.Load(d.cfg.StatePath)
	if err != nil {
		return err
	}
	return fn(st)
}

func (d *daemon) pollLoop(ctx context.Context, rf daemonFeed, feedID *string, endpoints *[]string, lastRevision *string, lastTTL **int, nextCacheReconcile *time.Time) error {
	d.logger.Debug("poll loop active", "feed_label", rf.label, "endpoints", len(*endpoints))
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		_ = d.withStateRead(func(st state.State) error {
			*endpoints = st.OrderEndpoints(rf.label, *endpoints)
			return nil
		})

		res, usedEndpoint, err := feed.FetchAnyEndpoints(ctx, *endpoints, rf.decryptURL, strings.TrimSpace(*lastRevision))
		if err != nil {
			if wf, ok := feed.AsWGFeedError(err); ok && !wf.Retriable {
				d.logger.Error("wg-feed non-retriable error; stopping polling", "feed_label", rf.label, "message", wf.Message)
				<-ctx.Done()
				return ctx.Err()
			}
			d.logger.Warn("poll fetch failed", "feed_label", rf.label, "err", redactTransportError(err))
			if err := d.maybeReconcileFromCache(ctx, rf, strings.TrimSpace(*feedID), *nextCacheReconcile); err == nil {
				*nextCacheReconcile = time.Now().Add(defaultReconcileTick)
			}
			d.logger.Debug("poll failure sleep", "feed_label", rf.label, "sleep", defaultTickOnFailure.String())
			sleep(ctx, defaultTickOnFailure)
			continue
		}
		if res.NotModified {
			s := d.pollSleep(rf, *lastTTL)
			if s < minTick {
				s = minTick
			}
			d.logger.Debug("poll not modified", "feed_label", rf.label, "revision", strings.TrimSpace(*lastRevision), "sleep", s.String())
			sleep(ctx, s)
			continue
		}
		d.logger.Debug("poll fetch succeeded", "feed_label", rf.label, "endpoint", feed.RedactURL(usedEndpoint), "revision", strings.TrimSpace(res.Revision), "ttl_seconds", res.TTLSeconds, "encrypted", res.Encrypted)

		*lastRevision = strings.TrimSpace(res.Revision)
		if *feedID == "" {
			*feedID = strings.TrimSpace(res.Feed.ID)
			if *feedID == "" {
				return fmt.Errorf("missing feed id")
			}
			if !d.claimFeedID(*feedID, rf.label) {
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
		if err := d.applyRemoteUpdate(ctx, rf, usedEndpoint, res.Feed, res.Revision, res.TTLSeconds, cached); err != nil {
			if wf, ok := feed.AsWGFeedError(err); ok && !wf.Retriable {
				d.logger.Error("wg-feed non-retriable error; stopping polling", "feed_label", rf.label, "message", wf.Message)
				<-ctx.Done()
				return ctx.Err()
			}
			d.logger.Warn("reconcile failed", "feed_label", rf.label, "err", redactTransportError(err))
		}
		d.logger.Debug("poll update applied", "feed_label", rf.label, "feed_id", strings.TrimSpace(res.Feed.ID), "tunnels", len(res.Feed.Tunnels), "revision", strings.TrimSpace(res.Revision))

		s := d.pollSleep(rf, *lastTTL)
		if s < minTick {
			s = minTick
		}
		d.logger.Debug("poll steady sleep", "feed_label", rf.label, "sleep", s.String())
		sleep(ctx, s)
	}
}

func (d *daemon) pollSleep(rf daemonFeed, lastTTL *int) time.Duration {
	if rf.cfg.Sync.Polling.Interval > 0 {
		return time.Duration(rf.cfg.Sync.Polling.Interval) * time.Second
	}
	if lastTTL != nil && *lastTTL > 0 {
		return time.Duration(*lastTTL) * time.Second
	}
	return defaultTickOnFailure
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	select {
	case <-ctx.Done():
		t.Stop()
	case <-t.C:
	}
}

func redactTransportError(err error) string {
	if err == nil {
		return ""
	}
	return transportURLRe.ReplaceAllStringFunc(err.Error(), func(raw string) string {
		return feed.RedactURL(raw)
	})
}

func decodeAndValidateSuccess(decryptURL string, body []byte) (model.FeedDocument, string, int, string, error) {
	var sr model.SuccessResponse
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&sr); err != nil {
		return model.FeedDocument{}, "", 0, "", err
	}
	if err := sr.Validate(); err != nil {
		return model.FeedDocument{}, "", 0, "", err
	}
	if sr.Encrypted {
		doc, err := feed.DecryptFeedDocumentForURL(decryptURL, sr.EncryptedData)
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

func (d *daemon) applyRemoteUpdate(ctx context.Context, rf daemonFeed, requestURL string, doc model.FeedDocument, revision string, ttl int, cachedEncryptedData string) error {
	feedID := strings.TrimSpace(doc.ID)
	if feedID == "" {
		return fmt.Errorf("missing feed id")
	}
	if msg := strings.TrimSpace(doc.Warning); msg != "" {
		d.logger.Warn("feed warning", "feed_label", rf.label, "message", msg)
	}
	return d.withStateSave(func(st *state.State) error {
		if st.Feeds == nil {
			st.Feeds = map[string]state.FeedState{}
		}
		fs := st.Feeds[rf.label]
		if fs.Backends == nil {
			fs.Backends = map[string]state.BackendState{}
		}
		fs.ID = feedID
		st.Feeds[rf.label] = fs

		st.ReconcileEndpointOrder(rf.label, doc.Endpoints, requestURL)
		fs = st.Feeds[rf.label]
		if fs.Backends == nil {
			fs.Backends = map[string]state.BackendState{}
		}

		v := ttl
		fs.TTLSeconds = &v
		fs.CachedEncryptedData = strings.TrimSpace(cachedEncryptedData)
		for backendLabel, backendCfg := range rf.cfg.Backends {
			bs := fs.Backends[backendLabel]
			if bs.Tunnels == nil {
				bs.Tunnels = map[string]state.TunnelState{}
			}
			bs.Type = string(backendCfg.Type)
			fs.Backends[backendLabel] = bs
		}
		st.Feeds[rf.label] = fs

		if strings.TrimSpace(revision) != "" && strings.TrimSpace(fs.LastReconciledRevision) == strings.TrimSpace(revision) && !rf.cfg.HasAnyEnabledOverride() {
			return nil
		}

		if err := client.ApplyFeed(ctx, st, rf.label, rf.backends, doc, rf.cfg.Tunnels, rf.cfg.BackendTunnelOverrides(), d.logger); err != nil {
			return err
		}
		fs = st.Feeds[rf.label]
		fs.LastReconciledRevision = strings.TrimSpace(revision)
		st.Feeds[rf.label] = fs
		return nil
	})
}

func (d *daemon) maybeReconcileFromCache(ctx context.Context, rf daemonFeed, feedID string, notBefore time.Time) error {
	if !notBefore.IsZero() && time.Now().Before(notBefore) {
		return fmt.Errorf("cache reconcile throttled")
	}
	feedID = strings.TrimSpace(feedID)
	if feedID == "" {
		return fmt.Errorf("no cached config")
	}
	return d.withStateSave(func(st *state.State) error {
		fs, ok := st.Feeds[rf.label]
		if !ok {
			return fmt.Errorf("no cached config")
		}
		if strings.TrimSpace(fs.ID) != "" && strings.TrimSpace(fs.ID) != feedID {
			return fmt.Errorf("cached feed id mismatch")
		}
		if strings.TrimSpace(fs.CachedEncryptedData) == "" {
			return fmt.Errorf("no cached config")
		}
		doc, err := feed.DecryptFeedDocumentForURL(rf.decryptURL, fs.CachedEncryptedData)
		if err != nil {
			return err
		}
		return client.ApplyFeed(ctx, st, rf.label, rf.backends, doc, rf.cfg.Tunnels, rf.cfg.BackendTunnelOverrides(), d.logger)
	})
}
