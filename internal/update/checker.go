// checker.go polls upstream for the latest release on a fixed interval and
// exposes a thread-safe snapshot of whether a newer version is available. It is
// decoupled from the GitHub client via the FetchFunc so it can be tested with a
// fake fetcher.
package update

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// FetchFunc retrieves the latest upstream release tag. priorETag enables
// conditional requests; notModified=true means "unchanged, keep prior tag"
// (tag may be empty in that case). A nil FetchFunc disables checking entirely.
type FetchFunc func(ctx context.Context, priorETag string) (tag, etag string, notModified bool, err error)

// State is an immutable snapshot of the checker's view of versions.
type State struct {
	Current   string    // the running binary's version
	Latest    string    // last known upstream release tag ("" until first success)
	Available bool      // Latest is a strictly newer release than Current
	CheckedAt time.Time // time of the last successful (non-error) check; zero if never
}

// Checker periodically refreshes State. The zero value is not usable — use
// NewChecker.
type Checker struct {
	current  string
	fetch    FetchFunc
	interval time.Duration
	logger   *slog.Logger

	mu        sync.RWMutex
	latest    string
	etag      string
	checkedAt time.Time
}

// NewChecker builds a Checker for the given running version. A nil fetch (e.g.
// update_repo unset) yields a checker that never reports an update — callers
// need not special-case the disabled path. interval below one minute is clamped
// up to avoid hammering the API.
func NewChecker(current string, fetch FetchFunc, interval time.Duration, logger *slog.Logger) *Checker {
	if logger == nil {
		logger = slog.Default()
	}
	if interval < time.Minute {
		interval = 10 * time.Minute
	}
	return &Checker{current: current, fetch: fetch, interval: interval, logger: logger}
}

// State returns the current snapshot.
func (c *Checker) State() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return State{
		Current:   c.current,
		Latest:    c.latest,
		Available: c.latest != "" && isNewer(c.latest, c.current),
		CheckedAt: c.checkedAt,
	}
}

// CheckNow performs one synchronous check. Fetch errors are logged and leave the
// prior state intact (fail soft — a transient API outage must not flip the
// banner off, nor crash the loop).
func (c *Checker) CheckNow(ctx context.Context) {
	if c.fetch == nil {
		return
	}
	c.mu.RLock()
	priorETag := c.etag
	c.mu.RUnlock()

	tag, etag, notModified, err := c.fetch(ctx, priorETag)
	if err != nil {
		c.logger.Warn("update.check", slog.String("err", err.Error()))
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkedAt = time.Now()
	if etag != "" {
		c.etag = etag
	}
	if notModified {
		return // keep prior c.latest
	}
	if tag != "" && tag != c.latest {
		c.logger.Info("update.available",
			slog.String("current", c.current),
			slog.String("latest", tag),
			slog.Bool("newer", isNewer(tag, c.current)),
		)
		c.latest = tag
	}
}

// Start runs an immediate check, then repeats every interval until ctx is
// cancelled. Returns a stop function (also cancellable via ctx).
func (c *Checker) Start(ctx context.Context) (stop func()) {
	rc, cancel := context.WithCancel(ctx)
	go func() {
		c.CheckNow(rc)
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-rc.Done():
				return
			case <-ticker.C:
				c.CheckNow(rc)
			}
		}
	}()
	return cancel
}
