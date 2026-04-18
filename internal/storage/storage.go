// Package storage wires the project's SQLite database and provides an atomic
// file write helper shared by content, backup, admin and other modules.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ErrCorruptDB is returned when the database file is unreadable and has been
// quarantined to <path>.corrupt.<timestamp>. A fresh empty DB is opened.
var ErrCorruptDB = errors.New("storage: database corrupt, quarantined and rebuilt")

// Store holds the opened SQLite handle and associated paths.
type Store struct {
	DB      *sql.DB
	DataDir string
}

// Open initialises SQLite at <dataDir>/data.sqlite, runs migrations, and
// returns a ready-to-use Store. If the DB file is corrupt it is moved aside
// and replaced with an empty one; caller receives ErrCorruptDB wrapped.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("storage: mkdir %s: %w", dataDir, err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "trash"), 0o700); err != nil {
		return nil, fmt.Errorf("storage: mkdir trash: %w", err)
	}

	// Writability probe: if dataDir is read-only we fail early with a clear msg.
	probe := filepath.Join(dataDir, ".wprobe")
	if err := os.WriteFile(probe, []byte{0}, 0o600); err != nil {
		return nil, fmt.Errorf("storage: data_dir %q not writable: %w", dataDir, err)
	}
	_ = os.Remove(probe)

	path := filepath.Join(dataDir, "data.sqlite")
	db, corruptErr, err := openOrRebuild(path)
	if err != nil {
		return nil, err
	}

	// Apply SQLite pragmas per architecture §R9.
	for _, pragma := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA wal_autocheckpoint=1000`,
		`PRAGMA foreign_keys=ON`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("storage: apply %s: %w", pragma, err)
		}
	}

	if err := migrate(context.Background(), db); err != nil {
		return nil, fmt.Errorf("storage: migrate: %w", err)
	}

	return &Store{DB: db, DataDir: dataDir}, corruptErr
}

// Close closes the underlying database.
func (s *Store) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

func openOrRebuild(path string) (*sql.DB, error, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: open: %w", err)
	}
	if err := db.Ping(); err == nil {
		// Run a trivial read to surface corruption that Ping may miss.
		if _, perr := db.Exec(`SELECT 1`); perr == nil {
			return db, nil, nil
		}
	}
	// Corruption path: move aside, reopen fresh.
	_ = db.Close()
	if _, statErr := os.Stat(path); statErr == nil {
		ts := time.Now().UTC().Format("20060102T150405Z")
		dest := fmt.Sprintf("%s.corrupt.%s", path, ts)
		if renErr := os.Rename(path, dest); renErr != nil {
			return nil, nil, fmt.Errorf("storage: quarantine corrupt db: %w", renErr)
		}
	}
	fresh, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: reopen: %w", err)
	}
	if pingErr := fresh.Ping(); pingErr != nil {
		return nil, nil, fmt.Errorf("storage: ping fresh db: %w", pingErr)
	}
	return fresh, ErrCorruptDB, nil
}
