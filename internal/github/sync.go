package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ReposSource returns the list of `owner/name` strings that should be
// synchronised. Implemented by content.Store.
type ReposSource interface {
	Repos() []string
}

// Syncer periodically refreshes cache entries for all registered repos.
type Syncer struct {
	client      *Client
	cache       *Cache
	repos       ReposSource
	interval    time.Duration
	readmeLimit int
	tokenSet    bool
	logger      *slog.Logger

	mu       sync.Mutex
	lastSync time.Time
}

// SyncerOpt configures a Syncer.
type SyncerOpt func(*Syncer)

// WithInterval overrides the sync period (default 30 minutes).
func WithInterval(d time.Duration) SyncerOpt { return func(s *Syncer) { s.interval = d } }

// WithLogger plugs in a custom slog.Logger.
func WithLogger(l *slog.Logger) SyncerOpt { return func(s *Syncer) { s.logger = l } }

// WithReadmeLimit sets the max excerpt length in runes (default 400).
func WithReadmeLimit(n int) SyncerOpt { return func(s *Syncer) { s.readmeLimit = n } }

// WithTokenConfigured signals whether a PAT was provided; used by safety-
// margin checks at startup. This is separate from the Client's token because
// the client already embeds it.
func WithTokenConfigured(b bool) SyncerOpt { return func(s *Syncer) { s.tokenSet = b } }

// NewSyncer constructs a Syncer.
func NewSyncer(c *Client, cache *Cache, repos ReposSource, opts ...SyncerOpt) *Syncer {
	s := &Syncer{
		client:      c,
		cache:       cache,
		repos:       repos,
		interval:    30 * time.Minute,
		readmeLimit: 400,
		logger:      slog.Default(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// LastSyncedAt reports the timestamp of the most recent sync attempt (not
// necessarily successful).
func (s *Syncer) LastSyncedAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSync
}

// Start kicks off the ticker loop and runs one immediate sync. Returns a
// stop function. Respects ctx cancellation.
func (s *Syncer) Start(ctx context.Context) (stop func()) {
	s.checkSafetyMargin()

	ticker := time.NewTicker(s.interval)
	runCtx, cancel := context.WithCancel(ctx)
	go func() {
		s.runOnce(runCtx)
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				s.runOnce(runCtx)
			}
		}
	}()
	return func() {
		cancel()
		ticker.Stop()
	}
}

// SyncNow triggers an immediate synchronous pass and returns when done.
// Exposed for tests and future admin "force sync" buttons.
func (s *Syncer) SyncNow(ctx context.Context) { s.runOnce(ctx) }

// runOnce iterates all repos, respecting cancellation and per-request errors.
func (s *Syncer) runOnce(ctx context.Context) {
	s.mu.Lock()
	s.lastSync = time.Now().UTC()
	s.mu.Unlock()

	for _, r := range s.repos.Repos() {
		if ctx.Err() != nil {
			return
		}
		s.syncOne(ctx, r)
	}
}

func (s *Syncer) syncOne(ctx context.Context, repo string) {
	owner, name, ok := splitRepo(repo)
	if !ok {
		s.logger.Error("github.sync.badrepo", slog.String("repo", repo))
		_ = s.cache.Upsert(ctx, CacheEntry{
			Repo:         repo,
			LastSyncedAt: time.Now().UTC(),
			LastError:    fmt.Sprintf("invalid repo identifier %q", repo),
		})
		return
	}
	existing, _ := s.cache.Get(ctx, repo)
	priorETag := ""
	if existing != nil {
		priorETag = existing.ETag
	}

	res, err := s.client.GetRepo(ctx, owner, name, priorETag)
	if err != nil {
		s.handleSyncError(ctx, repo, existing, err)
		return
	}
	var newETag string
	var info *RepoInfo
	if res.NotModified && existing != nil {
		// Preserve existing payload; just refresh last_synced_at.
		newETag = existing.ETag
		info = existing.Info
	} else if res.Info != nil {
		info = res.Info
		newETag = res.ETag
		// README fetch is best-effort.
		excerpt, rerr := s.client.GetReadmeExcerpt(ctx, owner, name, s.readmeLimit)
		if rerr != nil {
			s.logger.Warn("github.sync.readme",
				slog.String("repo", repo),
				slog.String("err", rerr.Error()))
		} else {
			info.ReadmeExcerpt = excerpt
		}
	}
	entry := CacheEntry{
		Repo:         repo,
		Info:         info,
		ETag:         newETag,
		LastSyncedAt: time.Now().UTC(),
	}
	if err := s.cache.Upsert(ctx, entry); err != nil {
		s.logger.Error("github.cache.upsert",
			slog.String("repo", repo),
			slog.String("err", err.Error()))
	}
}

func (s *Syncer) handleSyncError(ctx context.Context, repo string, existing *CacheEntry, err error) {
	// Rate limit: honour Retry-After by sleeping (bounded) before returning.
	var rl *RateLimitError
	if errors.As(err, &rl) {
		wait := rl.RetryAfter
		if wait <= 0 || wait > 5*time.Minute {
			wait = 5 * time.Minute
		}
		s.logger.Warn("github.sync.ratelimit",
			slog.String("repo", repo),
			slog.Duration("retry_after", wait))
		// Don't busy-wait during a test: just return; outer ticker schedules again.
	}
	s.logger.Error("github.sync.error",
		slog.String("repo", repo),
		slog.String("err", err.Error()))
	entry := CacheEntry{
		Repo:         repo,
		LastSyncedAt: time.Now().UTC(),
		LastError:    err.Error(),
	}
	if existing != nil {
		entry.Info = existing.Info
		entry.ETag = existing.ETag
	}
	_ = s.cache.Upsert(ctx, entry)
}

// checkSafetyMargin logs a WARN when the expected hourly API footprint
// exceeds 50% of the unauthenticated rate limit (60/h) and no token is set.
func (s *Syncer) checkSafetyMargin() {
	if s.tokenSet {
		return
	}
	repoCount := len(s.repos.Repos())
	if repoCount == 0 {
		return
	}
	// Each cycle performs one GetRepo + one GetReadme per repo.
	cyclesPerHour := float64(time.Hour) / float64(s.interval)
	requestsPerHour := float64(repoCount) * 2 * cyclesPerHour
	if requestsPerHour > 30 {
		s.logger.Warn("github.sync.safety_margin_warning",
			slog.Int("repo_count", repoCount),
			slog.Float64("expected_req_per_hour", requestsPerHour),
			slog.String("hint", "configure github_token or increase github_sync_interval_min"))
	}
}

func splitRepo(r string) (owner, name string, ok bool) {
	r = strings.TrimSpace(r)
	i := strings.IndexByte(r, '/')
	if i <= 0 || i == len(r)-1 {
		return "", "", false
	}
	return r[:i], r[i+1:], true
}
