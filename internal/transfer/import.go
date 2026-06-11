// import.go unpacks an export bundle into a data directory. It validates the
// manifest before touching disk and guards against path-traversal members.
// Intended to run on a target server with the blog-server process stopped, so
// that no live *sql.DB handle holds the old data.sqlite inode.
package transfer

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Errors classified for callers (CLI) to branch on via errors.Is.
var (
	// ErrBadFormat means the manifest is missing, malformed, or not a
	// blog-server export bundle.
	ErrBadFormat = errors.New("transfer: not a blog-server export bundle")
	// ErrUnsupportedVersion means the bundle schema is newer than this binary.
	ErrUnsupportedVersion = errors.New("transfer: unsupported bundle version")
	// ErrTargetNotEmpty means the data dir already holds data and force was
	// not set — refusing to clobber rather than silently overwrite.
	ErrTargetNotEmpty = errors.New("transfer: target data dir already populated")
)

// ImportBundle reads a gzip-tar bundle from r and unpacks it into dataDir.
// When force is false and any of the bundle's top-level entries already exist
// in dataDir, it returns ErrTargetNotEmpty without modifying anything it can
// detect up front. Returns the parsed manifest on success.
func ImportBundle(r io.Reader, dataDir string, force bool) (Manifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: gzip: %v", ErrBadFormat, err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)

	// The manifest is written first by WriteBundle; require it up front.
	manifest, first, err := readManifest(tr)
	if err != nil {
		return Manifest{}, err
	}
	if !force {
		if err := ensureTargetEmpty(dataDir, manifest.Entries); err != nil {
			return Manifest{}, err
		}
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("transfer: mkdir data dir: %w", err)
	}

	// first is the header following the manifest (or nil at EOF).
	for hdr := first; hdr != nil; {
		if err := extractMember(tr, hdr, dataDir, manifest.Entries); err != nil {
			return Manifest{}, err
		}
		hdr, err = tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Manifest{}, fmt.Errorf("transfer: read tar: %w", err)
		}
	}
	return manifest, nil
}

// readManifest reads and validates the leading manifest member, returning the
// manifest and the NEXT header (the first data member, or nil at EOF).
func readManifest(tr *tar.Reader) (Manifest, *tar.Header, error) {
	hdr, err := tr.Next()
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("%w: empty archive: %v", ErrBadFormat, err)
	}
	if filepath.ToSlash(hdr.Name) != manifestName {
		return Manifest{}, nil, fmt.Errorf("%w: first member is %q, want %s", ErrBadFormat, hdr.Name, manifestName)
	}
	var m Manifest
	if err := json.NewDecoder(tr).Decode(&m); err != nil {
		return Manifest{}, nil, fmt.Errorf("%w: manifest json: %v", ErrBadFormat, err)
	}
	if m.Format != FormatMagic {
		return Manifest{}, nil, fmt.Errorf("%w: format %q", ErrBadFormat, m.Format)
	}
	if m.Version > FormatVersion {
		return Manifest{}, nil, fmt.Errorf("%w: bundle v%d > supported v%d", ErrUnsupportedVersion, m.Version, FormatVersion)
	}
	next, err := tr.Next()
	if err == io.EOF {
		return m, nil, nil
	}
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("transfer: read tar: %w", err)
	}
	return m, next, nil
}

// ensureTargetEmpty returns ErrTargetNotEmpty if any declared entry already
// exists under dataDir.
func ensureTargetEmpty(dataDir string, entries []string) error {
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(dataDir, e)); err == nil {
			return fmt.Errorf("%w: %s exists", ErrTargetNotEmpty, e)
		}
	}
	return nil
}

// extractMember writes a single tar member into dataDir after verifying it
// falls within an allowed top-level entry and does not escape via "..".
func extractMember(tr *tar.Reader, hdr *tar.Header, dataDir string, entries []string) error {
	clean := filepath.Clean(filepath.ToSlash(hdr.Name))
	if !memberAllowed(clean, entries) {
		return fmt.Errorf("%w: stray member %q", ErrBadFormat, hdr.Name)
	}
	dest := filepath.Join(dataDir, filepath.FromSlash(clean))
	// Defence in depth against path traversal: the resolved path must stay
	// inside dataDir.
	root := filepath.Clean(dataDir)
	if dest != root && !strings.HasPrefix(dest, root+string(os.PathSeparator)) {
		return fmt.Errorf("%w: member escapes data dir: %q", ErrBadFormat, hdr.Name)
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(dest, 0o700)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return err
		}
		return writeFile(tr, dest, os.FileMode(hdr.Mode)&0o777)
	default:
		// Skip symlinks / devices / fifos: an export only carries dirs+files.
		return nil
	}
}

// memberAllowed reports whether clean (a slash path) belongs to one of the
// declared top-level entries.
func memberAllowed(clean string, entries []string) bool {
	for _, e := range entries {
		if clean == e || strings.HasPrefix(clean, e+"/") {
			return true
		}
	}
	return false
}

// writeFile streams the current tar member to dest with the given mode.
func writeFile(tr *tar.Reader, dest string, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // size bounded by trusted, authenticated bundle
		return err
	}
	return f.Close()
}
