// Package transfer implements full-site export/import bundles for migrating a
// blog-server instance between machines. A bundle is a gzipped tar containing
// a manifest plus the data subtrees (content/, images/, data.sqlite). Export
// streams the bundle to any io.Writer; import validates the manifest and
// unpacks into a data directory.
//
// Key dependencies: archive/tar + compress/gzip for the container, the same
// data_dir layout produced by internal/backup.
package transfer

// Manifest is the bundle's self-description, stored as manifest.json at the
// archive root. It lets the importer reject foreign or future-format archives
// before touching the filesystem.
type Manifest struct {
	// Format is a fixed magic string identifying blog-server export bundles.
	Format string `json:"format"`
	// Version is the bundle schema version. Bumped on incompatible changes.
	Version int `json:"version"`
	// CreatedAt is an RFC3339 timestamp stamped by the caller (the package
	// itself never reads the clock, to keep it deterministic for tests).
	CreatedAt string `json:"created_at"`
	// AppVersion records the producing binary's version for diagnostics.
	AppVersion string `json:"app_version"`
	// Entries lists the top-level data_dir members carried in the bundle.
	Entries []string `json:"entries"`
}

const (
	// manifestName is the archive member holding the JSON manifest.
	manifestName = "manifest.json"
	// FormatMagic identifies a blog-server export bundle.
	FormatMagic = "blog-server-export"
	// FormatVersion is the current bundle schema version.
	FormatVersion = 1
)

// DefaultEntries are the data_dir subtrees a full export carries. backups/ and
// trash/ are intentionally excluded: the former is regenerated on the target
// and the latter is soft-deleted content not worth migrating.
var DefaultEntries = []string{"content", "images", "data.sqlite"}
