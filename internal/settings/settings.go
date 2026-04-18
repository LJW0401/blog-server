// Package settings wraps the `site_settings` KV table. admin/ writes; public/
// reads. Keeping this in its own tiny package avoids an admin→public cycle.
package settings

import (
	"database/sql"
	"errors"
	"time"
)

// Store is a thin KV layer over site_settings.
type Store struct {
	db *sql.DB
}

// New wraps the given *sql.DB.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Get returns the stored value for k. (empty, false) on miss.
func (s *Store) Get(k string) (string, bool) {
	row := s.db.QueryRow(`SELECT v FROM site_settings WHERE k = ?`, k)
	var v []byte
	if err := row.Scan(&v); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false
		}
		return "", false
	}
	return string(v), true
}

// All dumps the full KV map.
func (s *Store) All() map[string]string {
	rows, err := s.db.Query(`SELECT k, v FROM site_settings`)
	if err != nil {
		return map[string]string{}
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		out[k] = string(v)
	}
	return out
}

// Set writes/replaces a key. Value "" is allowed (means clear).
func (s *Store) Set(k, v string) error {
	_, err := s.db.Exec(`INSERT INTO site_settings (k, v, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(k) DO UPDATE SET v=excluded.v, updated_at=excluded.updated_at`,
		k, []byte(v), time.Now().Unix())
	return err
}

// SetMany writes a batch of keys in a single transaction; all or nothing.
func (s *Store) SetMany(kvs map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().Unix()
	for k, v := range kvs {
		if _, err := tx.Exec(`INSERT INTO site_settings (k, v, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(k) DO UPDATE SET v=excluded.v, updated_at=excluded.updated_at`,
			k, []byte(v), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}
