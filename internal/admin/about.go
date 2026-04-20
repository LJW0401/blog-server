package admin

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/penguin/blog-server/internal/assets"
	"github.com/penguin/blog-server/internal/auth"
)

// AboutHandlers manages the single-file /about page source. Stored at
// <DataDir>/content/about.md as plain Markdown (no frontmatter) so it
// lives next to the content tree but is not indexed as a blog doc.
type AboutHandlers struct {
	Parent  *Handlers
	DataDir string
}

// path resolves the on-disk location of about.md.
func (a *AboutHandlers) path() string {
	return filepath.Join(a.DataDir, "content", "about.md")
}

// AboutPage handles GET /manage/about — renders the editor. First visit
// (file absent) prefills the textarea with the shipped default so the admin
// edits *from* a template instead of a blank page. Once the admin saves —
// even if saving an empty string — the on-disk file is authoritative and we
// stop injecting the default.
func (a *AboutHandlers) AboutPage(w http.ResponseWriter, r *http.Request) {
	sess, _ := a.Parent.Auth.ParseSession(r)
	var body string
	if b, err := os.ReadFile(a.path()); err == nil {
		body = string(b)
	} else {
		body = assets.DefaultAbout()
	}
	data := map[string]any{
		"Body": body,
		"CSRF": sess.CSRF,
		"Info": r.URL.Query().Get("m"),
	}
	if err := a.Parent.Tpl.Render(w, r, http.StatusOK, "admin_about_edit.html", data); err != nil {
		a.Parent.Logger.Error("admin.about.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// AboutSubmit handles POST /manage/about — validates CSRF, writes file
// atomically (temp file + rename) so readers never see a half-written page.
func (a *AboutHandlers) AboutSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := a.Parent.Auth.ParseSession(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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
	body := r.Form.Get("body")
	if err := writeAboutFile(a.path(), body); err != nil {
		a.Parent.Logger.Error("admin.about.write", slog.String("err", err.Error()))
		http.Error(w, "write failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/manage/about?m=saved", http.StatusSeeOther)
}

// writeAboutFile ensures the parent dir exists, writes to a temp sibling, and
// atomically renames into place. 0o600 perms match the rest of the repo.
func writeAboutFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
