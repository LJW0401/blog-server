package admin

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/content"
)

// Trash subdir conventions: files live under $DataDir/trash/<kind>/ where
// <kind> is one of TrashKindDoc / TrashKindProject / TrashKindPortfolio. This
// replaces the earlier flat trash/<name>.md layout (which encoded kind via a
// `proj-` filename prefix for projects only). MigrateFlatTrash handles the
// one-time move on first start after upgrade.
const (
	TrashKindDoc       = "docs"
	TrashKindProject   = "projects"
	TrashKindPortfolio = "portfolio"
)

// trashBasenameRe matches `YYYYMMDD-HHMMSS-<slug>.md` within a kind subdir.
// Slug is `[\w.-]+` — identical to the old rule.
var trashBasenameRe = regexp.MustCompile(`^(\d{8}-\d{6})-([\w.-]+)\.md$`)

// trashLegacyFlatRe matches the pre-migration flat layout. Used only by
// MigrateFlatTrash; runtime code reads the new layout.
var trashLegacyFlatRe = regexp.MustCompile(`^(\d{8}-\d{6})-(proj-)?([\w.-]+)\.md$`)

// TrashHandlers manages the soft-deleted content MD files that DocHandlers,
// ProjectHandlers and PortfolioHandlers rename into $DataDir/trash/<kind>/.
// It exposes a list page, plus restore (move back to content/<kind>/) and
// purge (permanent rm).
type TrashHandlers struct {
	Parent  *Handlers
	Content *content.Store
	DataDir string // absolute path; trash/ sits under it
}

// TrashEntry is what admin_trash.html consumes.
//
// Filename carries the `<kind>/<basename>` token the form posts back (e.g.
// "docs/20260419-101010-my-slug.md"). Kind is the human-displayable label
// matching the existing template's eq checks ("doc" / "project" /
// "portfolio").
type TrashEntry struct {
	Filename   string    // token: "<kind>/<basename>"
	Kind       string    // "doc" | "project" | "portfolio" (UI-facing)
	Slug       string    // original slug extracted from the basename
	TrashedAt  time.Time // parsed from the filename prefix (UTC)
	TrashedStr string    // pre-formatted: "2006-01-02 15:04:05 UTC"
	Size       int64     // bytes
}

// TrashList renders GET /manage/trash.
func (t *TrashHandlers) TrashList(w http.ResponseWriter, r *http.Request) {
	sess, ok := t.Parent.Auth.ParseSession(r)
	if !ok {
		http.Redirect(w, r, "/manage/login", http.StatusSeeOther)
		return
	}
	entries, err := t.scan()
	if err != nil {
		t.Parent.Logger.Error("admin.trash.scan", slog.String("err", err.Error()))
		http.Error(w, "scan trash failed", http.StatusInternalServerError)
		return
	}
	data := map[string]any{
		"Entries": entries,
		"CSRF":    sess.CSRF,
		"Info":    r.URL.Query().Get("info"),
		"Error":   r.URL.Query().Get("e"),
	}
	if err := t.Parent.Tpl.Render(w, r, http.StatusOK, "admin_trash.html", data); err != nil {
		t.Parent.Logger.Error("admin.trash.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// Restore handles POST /manage/trash/restore. Body: csrf, filename.
// filename is the token "<kind>/<basename>" emitted by the list page.
func (t *TrashHandlers) Restore(w http.ResponseWriter, r *http.Request) {
	sess, ok := t.Parent.Auth.ParseSession(r)
	if !ok {
		http.Redirect(w, r, "/manage/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !auth.CSRFValid(sess, r.Form.Get("csrf")) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	token := r.Form.Get("filename")
	src, kindDir, slug, err := t.resolve(token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	targetDir := filepath.Join(t.DataDir, "content", kindDir)
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Parent.Logger.Error("admin.trash.restore.mkdir", slog.String("err", err.Error()))
		http.Error(w, "mkdir failed", http.StatusInternalServerError)
		return
	}
	target := filepath.Join(targetDir, slug+".md")
	if _, err := os.Stat(target); err == nil {
		http.Redirect(w, r, "/manage/trash?e="+urlSafe(slug+" 已存在，先处理现有条目再恢复"), http.StatusSeeOther)
		return
	}
	if err := os.Rename(src, target); err != nil {
		t.Parent.Logger.Error("admin.trash.restore.rename", slog.String("err", err.Error()))
		http.Error(w, "restore failed", http.StatusInternalServerError)
		return
	}
	if err := t.Content.Reload(); err != nil {
		t.Parent.Logger.Warn("admin.trash.restore.reload", slog.String("err", err.Error()))
	}
	http.Redirect(w, r, "/manage/trash?info="+urlSafe("已恢复 "+slug), http.StatusSeeOther)
}

// Purge handles POST /manage/trash/purge. Body: csrf, filename. Permanent rm.
func (t *TrashHandlers) Purge(w http.ResponseWriter, r *http.Request) {
	sess, ok := t.Parent.Auth.ParseSession(r)
	if !ok {
		http.Redirect(w, r, "/manage/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !auth.CSRFValid(sess, r.Form.Get("csrf")) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	token := r.Form.Get("filename")
	src, _, slug, err := t.resolve(token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.Remove(src); err != nil {
		t.Parent.Logger.Error("admin.trash.purge.remove", slog.String("err", err.Error()))
		http.Error(w, "purge failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/manage/trash?info="+urlSafe("已彻底删除 "+slug), http.StatusSeeOther)
}

// --- Helpers ---------------------------------------------------------------

// resolve parses and validates a trash token posted by the form. Returns the
// absolute path, kind subdir ("docs" / "projects" / "portfolio"), and
// original slug. The token format is exactly `<kind>/<basename>`; anything
// else (path traversal, missing subdir, bad basename) is rejected.
func (t *TrashHandlers) resolve(token string) (srcAbs, kindDir, slug string, err error) {
	if token == "" {
		return "", "", "", fmt.Errorf("filename 不能为空")
	}
	parts := strings.SplitN(token, "/", 3)
	if len(parts) != 2 {
		return "", "", "", fmt.Errorf("filename 非法")
	}
	kindDir = parts[0]
	basename := parts[1]
	switch kindDir {
	case TrashKindDoc, TrashKindProject, TrashKindPortfolio:
	default:
		return "", "", "", fmt.Errorf("filename 非法")
	}
	m := trashBasenameRe.FindStringSubmatch(basename)
	if m == nil {
		return "", "", "", fmt.Errorf("filename 非法")
	}
	slug = m[2]

	trashDir := filepath.Join(t.DataDir, "trash", kindDir)
	trashAbs, err := filepath.Abs(trashDir)
	if err != nil {
		return "", "", "", fmt.Errorf("abs trash dir: %w", err)
	}
	src := filepath.Join(trashAbs, basename)
	clean, err := filepath.Abs(src)
	if err != nil {
		return "", "", "", fmt.Errorf("abs src: %w", err)
	}
	prefix := trashAbs + string(os.PathSeparator)
	if !strings.HasPrefix(clean, prefix) {
		return "", "", "", fmt.Errorf("filename 非法")
	}
	info, statErr := os.Stat(clean)
	if statErr != nil || info.IsDir() {
		return "", "", "", fmt.Errorf("文件不存在")
	}
	return clean, kindDir, slug, nil
}

// scan walks the three kind subdirs and returns entries sorted newest-first.
func (t *TrashHandlers) scan() ([]TrashEntry, error) {
	root := filepath.Join(t.DataDir, "trash")
	out := []TrashEntry{}
	for _, kd := range []string{TrashKindDoc, TrashKindProject, TrashKindPortfolio} {
		sub := filepath.Join(root, kd)
		dirEntries, err := os.ReadDir(sub)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, de := range dirEntries {
			if de.IsDir() {
				continue
			}
			m := trashBasenameRe.FindStringSubmatch(de.Name())
			if m == nil {
				continue
			}
			trashedAt, _ := time.Parse("20060102-150405", m[1])
			info, err := de.Info()
			if err != nil {
				continue
			}
			out = append(out, TrashEntry{
				Filename:   kd + "/" + de.Name(),
				Kind:       uiLabelForKind(kd),
				Slug:       m[2],
				TrashedAt:  trashedAt,
				TrashedStr: trashedAt.Format("2006-01-02 15:04:05 UTC"),
				Size:       info.Size(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TrashedAt.After(out[j].TrashedAt) })
	return out, nil
}

func uiLabelForKind(kindDir string) string {
	switch kindDir {
	case TrashKindDoc:
		return "doc"
	case TrashKindProject:
		return "project"
	case TrashKindPortfolio:
		return "portfolio"
	default:
		return kindDir
	}
}

// MigrateFlatTrash is a one-time migration that moves files from the legacy
// flat $DataDir/trash/*.md layout into the new
// $DataDir/trash/<kind>/*.md layout:
//
//   - Files with `proj-<slug>` pattern → trash/projects/<timestamp>-<slug>.md
//     (the `proj-` prefix is stripped; kind is now encoded by the parent dir)
//   - Other matching files → trash/docs/<timestamp>-<slug>.md
//   - Non-matching files are left in place
//
// The function is idempotent: once no flat .md files remain at the trash
// root, subsequent calls are no-ops. Errors moving individual files are
// logged and the migration continues with the next file, so one bad file
// can't block the rest.
func MigrateFlatTrash(dataDir string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	root := filepath.Join(dataDir, "trash")
	dirEntries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	moved := 0
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		if !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		m := trashLegacyFlatRe.FindStringSubmatch(de.Name())
		if m == nil {
			continue
		}
		timestamp := m[1]
		isProject := m[2] == "proj-"
		slug := m[3]
		var kindDir string
		if isProject {
			kindDir = TrashKindProject
		} else {
			kindDir = TrashKindDoc
		}
		newBasename := timestamp + "-" + slug + ".md"
		targetDir := filepath.Join(root, kindDir)
		if err := os.MkdirAll(targetDir, 0o700); err != nil {
			logger.Warn("admin.trash.migrate.mkdir",
				slog.String("dir", targetDir),
				slog.String("err", err.Error()))
			continue
		}
		src := filepath.Join(root, de.Name())
		dst := filepath.Join(targetDir, newBasename)
		if _, err := os.Stat(dst); err == nil {
			logger.Warn("admin.trash.migrate.dst_exists",
				slog.String("src", src),
				slog.String("dst", dst))
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			logger.Warn("admin.trash.migrate.rename",
				slog.String("src", src),
				slog.String("dst", dst),
				slog.String("err", err.Error()))
			continue
		}
		logger.Info("admin.trash.migrate.moved",
			slog.String("src", src),
			slog.String("dst", dst),
			slog.String("kind", kindDir))
		moved++
	}
	if moved > 0 {
		logger.Info("admin.trash.migrate.done", slog.Int("moved", moved))
	}
	return nil
}

// urlSafe URL-encodes a short status string for flash-redirect query params.
func urlSafe(s string) string { return url.QueryEscape(s) }
