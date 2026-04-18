// Package backup creates daily cold archives of content/, images/ and the
// SQLite database. Archives live under backups/ as YYYYMMDD.tar.gz and the
// newest 7 are retained.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	backupHour = 3 // 03:00 local
	keepCopies = 7
)

// Store wraps the dependencies a Backup service needs.
type Store struct {
	DataDir string
	DB      *sql.DB
	Logger  *slog.Logger
}

// New returns a Store. Missing logger is fine (defaults to slog.Default).
func New(dataDir string, db *sql.DB, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{DataDir: dataDir, DB: db, Logger: logger}
}

// Start schedules the daily backup loop. Returns a cancel function.
// The first run fires at the next 03:00 boundary; a missed boundary does not
// back-fire.
func (s *Store) Start(ctx context.Context) (cancel func()) {
	rc, stop := context.WithCancel(ctx)
	go s.loop(rc)
	return stop
}

func (s *Store) loop(ctx context.Context) {
	for {
		next := nextBoundary(time.Now())
		d := time.Until(next)
		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := s.RunNow(ctx); err != nil {
				s.Logger.Error("backup.run", slog.String("err", err.Error()))
			}
		}
	}
}

// RunNow performs an immediate backup regardless of schedule — used in tests
// and future admin "force backup" buttons.
func (s *Store) RunNow(ctx context.Context) error {
	outDir := filepath.Join(s.DataDir, "backups")
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return fmt.Errorf("backup: mkdir %s: %w", outDir, err)
	}
	// Checkpoint the WAL so the .sqlite file is a consistent snapshot.
	if s.DB != nil {
		if _, err := s.DB.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			s.Logger.Warn("backup.wal_checkpoint", slog.String("err", err.Error()))
		}
	}

	stamp := time.Now().UTC().Format("20060102")
	outPath := filepath.Join(outDir, stamp+".tar.gz")
	// If today's backup already exists, overwrite — idempotent.
	if err := writeTarGz(outPath, s.DataDir, []string{"content", "images", "data.sqlite"}); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	s.Logger.Info("backup.done", slog.String("path", outPath))

	if err := rotate(outDir, keepCopies); err != nil {
		s.Logger.Warn("backup.rotate", slog.String("err", err.Error()))
	}
	return nil
}

// nextBoundary returns the next instant at `backupHour`:00:00 in local time,
// strictly after `from`.
func nextBoundary(from time.Time) time.Time {
	loc := from.Location()
	next := time.Date(from.Year(), from.Month(), from.Day(), backupHour, 0, 0, 0, loc)
	if !next.After(from) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

// writeTarGz creates outPath containing the given dataDir subtrees. Missing
// entries are skipped with a warning.
func writeTarGz(outPath, dataDir string, entries []string) error {
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	gz := gzip.NewWriter(out)
	defer func() { _ = gz.Close() }()
	tw := tar.NewWriter(gz)
	defer func() { _ = tw.Close() }()

	for _, entry := range entries {
		abs := filepath.Join(dataDir, entry)
		if _, err := os.Stat(abs); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if err := addTree(tw, abs, entry); err != nil {
			return err
		}
	}
	return nil
}

func addTree(tw *tar.Writer, root, base string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(base, rel))
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = name
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		_, err = io.Copy(tw, f)
		return err
	})
}

// rotate deletes old backups keeping the newest `keep` by name.
func rotate(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var archives []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".tar.gz") {
			continue
		}
		archives = append(archives, n)
	}
	if len(archives) <= keep {
		return nil
	}
	sort.Strings(archives) // lexical == chronological (YYYYMMDD.tar.gz)
	victims := archives[:len(archives)-keep]
	for _, v := range victims {
		if err := os.Remove(filepath.Join(dir, v)); err != nil {
			return err
		}
	}
	return nil
}
