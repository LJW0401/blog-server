// Package content scans Markdown files under content/docs and
// content/projects, parses YAML frontmatter and exposes an in-memory index
// of published/draft/archived entries. Updates are driven by an fsnotify
// watcher with debounce.
package content

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Status is the publication state declared in frontmatter.
type Status string

const (
	// Shared
	StatusArchived Status = "archived"
	// Docs
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	// Projects
	StatusActive     Status = "active"
	StatusDeveloping Status = "developing"
)

// Kind distinguishes docs and projects.
type Kind string

const (
	KindDoc     Kind = "doc"
	KindProject Kind = "project"
)

// Errors surfaced during loading. Callers use errors.Is for classification.
var (
	ErrNoFrontmatter   = errors.New("content: missing frontmatter")
	ErrInvalidFM       = errors.New("content: invalid frontmatter")
	ErrMissingRequired = errors.New("content: required field missing")
	ErrInvalidSlug     = errors.New("content: invalid slug")
	ErrDuplicateSlug   = errors.New("content: duplicate slug")
)

// Entry represents a parsed Markdown file.
type Entry struct {
	Kind     Kind
	Path     string
	Slug     string
	Title    string
	Tags     []string
	Category string
	Created  time.Time
	Updated  time.Time
	Status   Status
	Featured bool
	Excerpt  string
	Body     string // raw Markdown after frontmatter
	// Project-specific
	Repo        string // owner/name
	DisplayName string
	DisplayDesc string
	Stack       []string
}

// Index is the in-memory collection indexed by kind+slug.
type Index struct {
	mu    sync.RWMutex
	items map[string]*Entry // key = string(kind)+"/"+slug
}

func newIndex() *Index { return &Index{items: map[string]*Entry{}} }

// Get returns an entry or (nil, false).
func (i *Index) Get(kind Kind, slug string) (*Entry, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	e, ok := i.items[string(kind)+"/"+slug]
	return e, ok
}

// List returns all entries of the given kind sorted by Updated desc.
// Callers filter on Status themselves.
func (i *Index) List(kind Kind) []*Entry {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]*Entry, 0, len(i.items))
	for _, e := range i.items {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		return out[a].Updated.After(out[b].Updated)
	})
	return out
}

// Len reports how many entries are currently indexed.
func (i *Index) Len() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.items)
}

// set replaces or inserts an entry; returns true if a duplicate slug within
// the same kind already exists (caller decides what to do).
func (i *Index) set(e *Entry) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	key := string(e.Kind) + "/" + e.Slug
	_, existed := i.items[key]
	i.items[key] = e
	return existed
}

// --- Loading ----------------------------------------------------------------

// Store is the top-level content registry. Use New() to construct.
type Store struct {
	root   string // content/
	logger *slog.Logger
	docs   *Index
	projs  *Index
}

// New creates a Store rooted at dataDir/content.
func New(dataDir string, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{
		root:   filepath.Join(dataDir, "content"),
		logger: logger,
		docs:   newIndex(),
		projs:  newIndex(),
	}
}

// Docs returns the documents index.
func (s *Store) Docs() *Index { return s.docs }

// Projects returns the projects index.
func (s *Store) Projects() *Index { return s.projs }

// Repos implements github.ReposSource: returns the set of `owner/name`
// identifiers declared by currently indexed project entries (including
// archived ones, so stats/caches don't lose history).
func (s *Store) Repos() []string {
	list := s.projs.List(KindProject)
	out := make([]string, 0, len(list))
	for _, e := range list {
		if e.Repo != "" {
			out = append(out, e.Repo)
		}
	}
	return out
}

// Reload performs a full scan of the content directory. Files with parse or
// validation errors are skipped and logged; the rest are indexed. A duplicate
// slug within a single kind is a fatal condition and returns an error so
// startup can fail fast (per requirement 2.2.1).
func (s *Store) Reload() error {
	newDocs := newIndex()
	newProjs := newIndex()

	if err := s.scanKind(filepath.Join(s.root, "docs"), KindDoc, newDocs); err != nil {
		return err
	}
	if err := s.scanKind(filepath.Join(s.root, "projects"), KindProject, newProjs); err != nil {
		return err
	}

	// Swap atomically by grabbing the indexes' locks once.
	s.docs.mu.Lock()
	s.docs.items = newDocs.items
	s.docs.mu.Unlock()
	s.projs.mu.Lock()
	s.projs.items = newProjs.items
	s.projs.mu.Unlock()
	return nil
}

func (s *Store) scanKind(dir string, kind Kind, idx *Index) error {
	// Missing directory is tolerated: service still starts with empty index.
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("content: readdir %s: %w", dir, err)
	}
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, de.Name())
		e, err := s.loadFile(path, kind)
		if err != nil {
			s.logger.Error("content.load",
				slog.String("path", path),
				slog.String("err", err.Error()))
			continue
		}
		if idx.set(e) {
			return fmt.Errorf("%w: %s conflicts with existing entry in %s",
				ErrDuplicateSlug, path, kind)
		}
	}
	return nil
}

// loadFile parses a single Markdown file into an Entry.
func (s *Store) loadFile(path string, kind Kind) (*Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	fmRaw, body, err := splitFrontmatter(f)
	if err != nil {
		return nil, err
	}

	// Unmarshal frontmatter with strict mode.
	dec := yaml.NewDecoder(bytes.NewReader(fmRaw))
	dec.KnownFields(true)
	var meta rawMeta
	if err := dec.Decode(&meta); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidFM, path, err)
	}

	e, err := meta.toEntry(kind, path, body)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// rawMeta mirrors the combined doc+project frontmatter schema. Unknown fields
// are rejected via KnownFields(true).
type rawMeta struct {
	Title       string    `yaml:"title"`
	Slug        string    `yaml:"slug"`
	Tags        []string  `yaml:"tags"`
	Category    string    `yaml:"category"`
	Created     time.Time `yaml:"created"`
	Updated     time.Time `yaml:"updated"`
	Status      string    `yaml:"status"`
	Featured    bool      `yaml:"featured"`
	Excerpt     string    `yaml:"excerpt"`
	Repo        string    `yaml:"repo"`
	DisplayName string    `yaml:"display_name"`
	DisplayDesc string    `yaml:"display_desc"`
	Stack       []string  `yaml:"stack"`
}

func (m rawMeta) toEntry(kind Kind, path, body string) (*Entry, error) {
	e := &Entry{
		Kind:        kind,
		Path:        path,
		Title:       strings.TrimSpace(m.Title),
		Slug:        strings.TrimSpace(m.Slug),
		Tags:        m.Tags,
		Category:    strings.TrimSpace(m.Category),
		Created:     m.Created,
		Updated:     m.Updated,
		Status:      parseStatus(m.Status),
		Featured:    m.Featured,
		Excerpt:     strings.TrimSpace(m.Excerpt),
		Body:        body,
		Repo:        strings.TrimSpace(m.Repo),
		DisplayName: strings.TrimSpace(m.DisplayName),
		DisplayDesc: strings.TrimSpace(m.DisplayDesc),
		Stack:       m.Stack,
	}
	if kind == KindDoc {
		if e.Title == "" {
			return nil, fmt.Errorf("%w: title (%s)", ErrMissingRequired, path)
		}
	}
	if kind == KindProject {
		if e.Repo == "" {
			return nil, fmt.Errorf("%w: repo (%s)", ErrMissingRequired, path)
		}
	}
	if e.Slug == "" {
		return nil, fmt.Errorf("%w: slug (%s)", ErrMissingRequired, path)
	}
	if !isValidSlug(e.Slug) {
		return nil, fmt.Errorf("%w: %q in %s", ErrInvalidSlug, e.Slug, path)
	}
	if e.Updated.IsZero() {
		e.Updated = e.Created
	}
	if e.Excerpt == "" {
		e.Excerpt = firstChars(body, 120)
	}
	return e, nil
}

func parseStatus(s string) Status {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "draft":
		return StatusDraft
	case "archived":
		return StatusArchived
	case "active":
		return StatusActive
	case "developing":
		return StatusDeveloping
	default:
		return StatusPublished
	}
}

func isValidSlug(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
		if !ok {
			return false
		}
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	return true
}

func firstChars(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return strings.TrimSpace(string(runes[:n])) + "…"
}

// splitFrontmatter reads a reader and returns (frontmatter bytes, body string).
// Expects `---\n ... \n---\n` at the very top. Files without a fence are
// treated as body-only; we return ErrNoFrontmatter so the caller can decide
// whether this is fatal (it is, for this project).
func splitFrontmatter(r io.Reader) ([]byte, string, error) {
	br := bufio.NewReader(r)
	first, err := br.ReadString('\n')
	if err != nil {
		return nil, "", fmt.Errorf("%w: empty file", ErrNoFrontmatter)
	}
	if strings.TrimRight(first, "\r\n") != "---" {
		return nil, "", ErrNoFrontmatter
	}
	var fm bytes.Buffer
	for {
		line, err := br.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "---" {
			body, _ := io.ReadAll(br)
			return fm.Bytes(), string(body), nil
		}
		fm.WriteString(line)
		if err == io.EOF {
			return nil, "", fmt.Errorf("%w: unterminated fence", ErrInvalidFM)
		}
		if err != nil {
			return nil, "", fmt.Errorf("%w: read: %v", ErrInvalidFM, err)
		}
	}
}
