// export.go streams a full-site bundle (manifest + data subtrees) as gzipped
// tar to an arbitrary writer. Used by the admin "导出" download.
package transfer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteBundle streams a gzip-compressed tar of the export to w. The manifest is
// written first so the importer can validate before unpacking. db may be nil;
// when set, a WAL checkpoint runs so the copied data.sqlite is a consistent
// snapshot. createdAt/appVersion are stamped into the manifest by the caller —
// this package never reads the clock.
func WriteBundle(ctx context.Context, w io.Writer, dataDir, createdAt, appVersion string, db *sql.DB) error {
	if db != nil {
		// Flush the WAL into the main db file so a raw file copy is consistent.
		if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			return fmt.Errorf("transfer: wal checkpoint: %w", err)
		}
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)

	manifest := Manifest{
		Format:     FormatMagic,
		Version:    FormatVersion,
		CreatedAt:  createdAt,
		AppVersion: appVersion,
		Entries:    DefaultEntries,
	}
	if err := writeManifest(tw, manifest); err != nil {
		return err
	}

	for _, entry := range DefaultEntries {
		abs := filepath.Join(dataDir, entry)
		info, err := os.Stat(abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // a fresh instance may lack images/ etc.
			}
			return fmt.Errorf("transfer: stat %s: %w", entry, err)
		}
		if err := addTree(tw, abs, entry, info); err != nil {
			return fmt.Errorf("transfer: pack %s: %w", entry, err)
		}
	}

	// Close in order: tar trailer, then gzip trailer. Defer would reverse a
	// premature return into the wrong order, so close explicitly.
	if err := tw.Close(); err != nil {
		return fmt.Errorf("transfer: close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("transfer: close gzip: %w", err)
	}
	return nil
}

// writeManifest emits the JSON manifest as the first tar member.
func writeManifest(tw *tar.Writer, m Manifest) error {
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("transfer: marshal manifest: %w", err)
	}
	hdr := &tar.Header{
		Name:     manifestName,
		Mode:     0o600,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("transfer: manifest header: %w", err)
	}
	if _, err := tw.Write(body); err != nil {
		return fmt.Errorf("transfer: manifest body: %w", err)
	}
	return nil
}

// addTree walks root and writes every regular file and directory under the
// archive path `base`. Symlinks and other irregular files are skipped.
func addTree(tw *tar.Writer, root, base string, rootInfo os.FileInfo) error {
	if !rootInfo.IsDir() {
		return addFile(tw, root, base, rootInfo) // single file, e.g. data.sqlite
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(base, rel))
		return addFile(tw, path, name, info)
	})
}

// addFile writes one filesystem entry as the tar member `name`.
func addFile(tw *tar.Writer, path, name string, info os.FileInfo) error {
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = name
	if info.IsDir() && name != "" {
		hdr.Name = name + "/"
	}
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
}
