package admin

import (
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

// TrashHandlers manages the soft-deleted content MD files that DocHandlers.DeleteDoc
// and ProjectHandlers.ReposDelete rename into $DataDir/trash/. It exposes a list
// page, plus restore (move back to content/<kind>/) and purge (permanent rm).
type TrashHandlers struct {
	Parent  *Handlers
	Content *content.Store
	DataDir string // absolute path; trash/ sits under it
}

// trashFileNameRe matches the filenames emitted by DeleteDoc / ReposDelete:
//
//	docs:     20060102-150405-<slug>.md
//	projects: 20060102-150405-proj-<slug>.md
//
// Slug is `[\w.-]+` — matches what extractSlug/extractSlugFromBody accept.
// Anchoring at both ends and forbidding `/` rules out path traversal.
var trashFileNameRe = regexp.MustCompile(`^(\d{8}-\d{6})-(proj-)?([\w.-]+)\.md$`)

// TrashEntry is what admin_trash.html consumes.
type TrashEntry struct {
	Filename   string    // as stored on disk (also the token POST-ed back to us)
	Kind       string    // "doc" or "project"
	Slug       string    // original slug extracted from the name
	TrashedAt  time.Time // parsed from the filename prefix (UTC)
	TrashedStr string    // pre-formatted for the template: "2006-01-02 15:04:05 UTC"
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
// Moves the file back to content/docs/<slug>.md or content/projects/<slug>.md,
// refuses if the target already exists.
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
	name := r.Form.Get("filename")
	src, kind, slug, err := t.resolve(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var targetDir string
	switch kind {
	case "doc":
		targetDir = filepath.Join(t.DataDir, "content", "docs")
	case "project":
		targetDir = filepath.Join(t.DataDir, "content", "projects")
	default:
		http.Error(w, "unknown kind", http.StatusBadRequest)
		return
	}
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
	name := r.Form.Get("filename")
	src, _, slug, err := t.resolve(name)
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

// resolve parses and validates a filename POST-ed by a form, returning the
// absolute trash path, kind, and original slug. The file must exist inside
// $DataDir/trash/ and match the expected naming pattern.
func (t *TrashHandlers) resolve(filename string) (srcAbs, kind, slug string, err error) {
	if filename == "" {
		return "", "", "", fmt.Errorf("filename 不能为空")
	}
	m := trashFileNameRe.FindStringSubmatch(filename)
	if m == nil {
		return "", "", "", fmt.Errorf("filename 非法")
	}
	if m[2] == "proj-" {
		kind = "project"
	} else {
		kind = "doc"
	}
	slug = m[3]

	trashDir := filepath.Join(t.DataDir, "trash")
	trashAbs, err := filepath.Abs(trashDir)
	if err != nil {
		return "", "", "", fmt.Errorf("abs trash dir: %w", err)
	}
	src := filepath.Join(trashAbs, filename)
	clean, err := filepath.Abs(src)
	if err != nil {
		return "", "", "", fmt.Errorf("abs src: %w", err)
	}
	// Defence in depth: even though the regex forbids `/`, verify the cleaned
	// path really is a direct child of trashDir.
	prefix := trashAbs + string(os.PathSeparator)
	if !strings.HasPrefix(clean, prefix) {
		return "", "", "", fmt.Errorf("filename 非法")
	}
	info, statErr := os.Stat(clean)
	if statErr != nil || info.IsDir() {
		return "", "", "", fmt.Errorf("文件不存在")
	}
	return clean, kind, slug, nil
}

// scan lists trash entries sorted newest-first.
func (t *TrashHandlers) scan() ([]TrashEntry, error) {
	trashDir := filepath.Join(t.DataDir, "trash")
	dirEntries, err := os.ReadDir(trashDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]TrashEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		m := trashFileNameRe.FindStringSubmatch(de.Name())
		if m == nil {
			continue // ignore files that don't match our naming convention
		}
		kind := "doc"
		if m[2] == "proj-" {
			kind = "project"
		}
		trashedAt, _ := time.Parse("20060102-150405", m[1])
		info, err := de.Info()
		if err != nil {
			continue
		}
		out = append(out, TrashEntry{
			Filename:   de.Name(),
			Kind:       kind,
			Slug:       m[3],
			TrashedAt:  trashedAt,
			TrashedStr: trashedAt.Format("2006-01-02 15:04:05 UTC"),
			Size:       info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TrashedAt.After(out[j].TrashedAt) })
	return out, nil
}

// urlSafe URL-encodes a short status string for flash-redirect query params.
func urlSafe(s string) string { return url.QueryEscape(s) }
