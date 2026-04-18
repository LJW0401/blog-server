// Package stats records per-document read counts. Deduplication uses a
// sha256(ip+ua) fingerprint scoped per slug with a 60-minute sliding window.
// Crawler user-agents are excluded from counting.
package stats

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Dedup window per requirement 2.8.
const DedupWindow = 60 * time.Minute

// Crawler UA needles — substrings treated case-insensitively. Covers the
// common search/social bots; not exhaustive and not meant to be.
var crawlerNeedles = []string{
	"googlebot", "bingbot", "duckduckbot", "baiduspider", "yandexbot",
	"facebookexternalhit", "slackbot", "twitterbot", "linkedinbot",
	"applebot", "petalbot", "semrushbot", "ahrefsbot", "mj12bot",
	"ia_archiver", "sogou",
}

// Store wraps the SQLite handle with stats-scoped operations.
type Store struct {
	db     *sql.DB
	logger *slog.Logger
}

// New returns a Store. Nil logger is safe (defaults to slog.Default).
func New(db *sql.DB, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{db: db, logger: logger}
}

// RecordRead increments the read counter for slug if the given fingerprint
// hasn't been seen in the last DedupWindow for that slug. Crawler UAs are
// skipped. DB errors are logged but not returned so the caller's page render
// does not fail on stats hiccups (requirement 2.8 explicitly allows losing
// counter accuracy).
func (s *Store) RecordRead(ctx context.Context, slug, ip, userAgent string) {
	if slug == "" {
		return
	}
	if IsCrawler(userAgent) {
		return
	}
	fp := fingerprint(ip, userAgent)
	now := time.Now().UTC().Unix()
	cutoff := now - int64(DedupWindow.Seconds())

	// If fingerprint already counted within window, skip.
	row := s.db.QueryRowContext(ctx,
		`SELECT seen_at FROM read_fingerprints WHERE fp=? AND slug=?`, fp, slug)
	var lastSeen int64
	if err := row.Scan(&lastSeen); err == nil {
		if lastSeen >= cutoff {
			return
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		s.logger.Error("stats.lookup_fp", slog.String("err", err.Error()))
		return
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger.Error("stats.begin_tx", slog.String("err", err.Error()))
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO read_fingerprints (fp, slug, seen_at) VALUES (?, ?, ?)
		ON CONFLICT(fp, slug) DO UPDATE SET seen_at=excluded.seen_at`,
		fp, slug, now); err != nil {
		s.logger.Error("stats.upsert_fp", slog.String("err", err.Error()))
		return
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO read_counts (slug, count) VALUES (?, 1)
		ON CONFLICT(slug) DO UPDATE SET count=count+1`,
		slug); err != nil {
		s.logger.Error("stats.upsert_count", slog.String("err", err.Error()))
		return
	}
	if err := tx.Commit(); err != nil {
		s.logger.Error("stats.commit", slog.String("err", err.Error()))
	}
}

// Count returns the current read count for slug, or 0 on miss/error.
func (s *Store) Count(ctx context.Context, slug string) int {
	row := s.db.QueryRowContext(ctx, `SELECT count FROM read_counts WHERE slug=?`, slug)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0
	}
	return n
}

// Counts returns a map slug→count for the given slug set. Slugs not in the
// DB map to 0. Implementation: one IN-clause query.
func (s *Store) Counts(ctx context.Context, slugs []string) map[string]int {
	out := make(map[string]int, len(slugs))
	if len(slugs) == 0 {
		return out
	}
	placeholders := make([]string, len(slugs))
	args := make([]any, len(slugs))
	for i, sl := range slugs {
		placeholders[i] = "?"
		args[i] = sl
		out[sl] = 0
	}
	query := `SELECT slug, count FROM read_counts WHERE slug IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var slug string
		var n int
		if err := rows.Scan(&slug, &n); err != nil {
			continue
		}
		out[slug] = n
	}
	return out
}

// --- Helpers ---------------------------------------------------------------

func fingerprint(ip, ua string) string {
	h := sha256.Sum256([]byte(ip + "|" + ua))
	return hex.EncodeToString(h[:8])
}

// IsCrawler reports whether ua matches a known bot substring.
func IsCrawler(ua string) bool {
	if ua == "" {
		return false
	}
	lower := strings.ToLower(ua)
	for _, n := range crawlerNeedles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

// RemoteIP extracts the client IP from an HTTP request, preferring the first
// entry of X-Forwarded-For (set by the reverse proxy) over RemoteAddr.
func RemoteIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		if i := strings.IndexByte(xf, ','); i > 0 {
			return strings.TrimSpace(xf[:i])
		}
		return strings.TrimSpace(xf)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		return host[:i]
	}
	return host
}
