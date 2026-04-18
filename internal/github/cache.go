package github

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// CacheEntry is what we return to upstream consumers (public handlers).
type CacheEntry struct {
	Repo         string
	Info         *RepoInfo
	ETag         string
	LastSyncedAt time.Time
	LastError    string // human-readable; empty on success
}

// Cache is a thin layer over the SQLite `github_cache` table.
type Cache struct {
	db *sql.DB
}

// NewCache wraps a *sql.DB created by internal/storage.
func NewCache(db *sql.DB) *Cache { return &Cache{db: db} }

// Upsert stores/replaces a repository's cache row.
func (c *Cache) Upsert(ctx context.Context, e CacheEntry) error {
	payload := struct {
		Info      *RepoInfo `json:"info"`
		LastError string    `json:"last_error,omitempty"`
	}{Info: e.Info, LastError: e.LastError}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cache.upsert: marshal: %w", err)
	}
	_, err = c.db.ExecContext(ctx, `INSERT INTO github_cache (repo, payload, etag, last_synced_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(repo) DO UPDATE SET
			payload = excluded.payload,
			etag = excluded.etag,
			last_synced_at = excluded.last_synced_at`,
		e.Repo, string(b), e.ETag, e.LastSyncedAt.UTC().Unix())
	return err
}

// Get returns the cache row for repo, or (nil, nil) if not yet synced.
func (c *Cache) Get(ctx context.Context, repo string) (*CacheEntry, error) {
	row := c.db.QueryRowContext(ctx, `SELECT payload, etag, last_synced_at FROM github_cache WHERE repo = ?`, repo)
	var payloadStr, etag string
	var ts int64
	if err := row.Scan(&payloadStr, &etag, &ts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var payload struct {
		Info      *RepoInfo `json:"info"`
		LastError string    `json:"last_error,omitempty"`
	}
	if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
		return nil, err
	}
	return &CacheEntry{
		Repo:         repo,
		Info:         payload.Info,
		ETag:         etag,
		LastSyncedAt: time.Unix(ts, 0),
		LastError:    payload.LastError,
	}, nil
}

// List returns all cache rows.
func (c *Cache) List(ctx context.Context) ([]*CacheEntry, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT repo, payload, etag, last_synced_at FROM github_cache`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*CacheEntry
	for rows.Next() {
		var repo, payloadStr, etag string
		var ts int64
		if err := rows.Scan(&repo, &payloadStr, &etag, &ts); err != nil {
			return nil, err
		}
		var payload struct {
			Info      *RepoInfo `json:"info"`
			LastError string    `json:"last_error,omitempty"`
		}
		_ = json.Unmarshal([]byte(payloadStr), &payload)
		out = append(out, &CacheEntry{
			Repo:         repo,
			Info:         payload.Info,
			ETag:         etag,
			LastSyncedAt: time.Unix(ts, 0),
			LastError:    payload.LastError,
		})
	}
	return out, rows.Err()
}

// Delete removes a cache row (called when a project is unregistered).
func (c *Cache) Delete(ctx context.Context, repo string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM github_cache WHERE repo = ?`, repo)
	return err
}
