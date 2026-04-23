package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// Migrations are applied in order; each file assumes the previous ones ran.
var migrations = []string{
	// 1: schema_version bookkeeping (set up by migrate itself)
	// 2: initial tables
	`CREATE TABLE IF NOT EXISTS site_settings (
		k TEXT PRIMARY KEY,
		v BLOB,
		updated_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS github_cache (
		repo TEXT PRIMARY KEY,
		payload TEXT NOT NULL,
		etag TEXT,
		last_synced_at INTEGER NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS read_counts (
		slug TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS read_fingerprints (
		fp TEXT NOT NULL,
		slug TEXT NOT NULL,
		seen_at INTEGER NOT NULL,
		PRIMARY KEY (fp, slug)
	)`,
	`CREATE TABLE IF NOT EXISTS login_failures (
		ip TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0,
		window_end_at INTEGER NOT NULL
	)`,
	// v1.6.2: 会话服务端记录，支持"登陆设备管理 + 撤销"。
	// 老的纯 HMAC cookie 迁移后会因 sid 校验失败被拒绝，管理员需要重新登陆一次。
	`CREATE TABLE IF NOT EXISTS sessions (
		sid TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		user_agent TEXT NOT NULL,
		ip TEXT NOT NULL,
		issued_at INTEGER NOT NULL,
		revoked_at INTEGER
	)`,
}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		id INTEGER PRIMARY KEY CHECK (id=1),
		version INTEGER NOT NULL
	)`); err != nil {
		return err
	}
	var cur int
	row := db.QueryRowContext(ctx, `SELECT version FROM schema_version WHERE id=1`)
	if err := row.Scan(&cur); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("read schema_version: %w", err)
	}
	target := len(migrations)
	if cur >= target {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i := cur; i < target; i++ {
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (id, version) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET version=excluded.version`, target); err != nil {
		return fmt.Errorf("update schema_version: %w", err)
	}
	return tx.Commit()
}
