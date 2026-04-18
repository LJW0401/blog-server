package admin

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/storage"
)

// ContentRoot is the path admin handlers write MD files into. Caller
// (main.go) sets it after construction. Default: DataDir/content/docs.
type DocHandlers struct {
	Parent  *Handlers
	Content *content.Store
	DataDir string // absolute path; trash/ and content/ sit under it
}

// docEditData is what admin_doc_edit.html consumes.
type docEditData struct {
	IsNew bool
	Slug  string
	Body  string // full MD source including frontmatter
	CSRF  string
	Error string
	Kind  string // "doc" or "project"
}

// --- List ------------------------------------------------------------------

// DocsList handles GET /manage/docs.
func (d *DocHandlers) DocsList(w http.ResponseWriter, r *http.Request) {
	sess, _ := d.Parent.Auth.ParseSession(r)
	entries := d.Content.Docs().List(content.KindDoc)
	data := map[string]any{
		"Docs":      entries,
		"CSRF":      sess.CSRF,
		"Kind":      "doc",
		"BasePath":  "/manage/docs",
		"NewLabel":  "新建文档",
		"EditLabel": "编辑",
	}
	if err := d.Parent.Tpl.Render(w, r, http.StatusOK, "admin_docs_list.html", data); err != nil {
		d.Parent.Logger.Error("admin.docs.list", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// NewDoc handles GET /manage/docs/new.
func (d *DocHandlers) NewDoc(w http.ResponseWriter, r *http.Request) {
	sess, _ := d.Parent.Auth.ParseSession(r)
	body := defaultDocFrontmatter()
	data := docEditData{IsNew: true, Slug: "", Body: body, CSRF: sess.CSRF, Kind: "doc"}
	d.renderEditor(w, r, data)
}

// EditDoc handles GET /manage/docs/:slug/edit.
func (d *DocHandlers) EditDoc(w http.ResponseWriter, r *http.Request) {
	slug := extractSlug(r.URL.Path, "/manage/docs/", "/edit")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	e, ok := d.Content.Docs().Get(content.KindDoc, slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	raw, err := os.ReadFile(e.Path)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	sess, _ := d.Parent.Auth.ParseSession(r)
	data := docEditData{IsNew: false, Slug: slug, Body: string(raw), CSRF: sess.CSRF, Kind: "doc"}
	d.renderEditor(w, r, data)
}

func (d *DocHandlers) renderEditor(w http.ResponseWriter, r *http.Request, data docEditData) {
	if err := d.Parent.Tpl.Render(w, r, http.StatusOK, "admin_doc_edit.html", data); err != nil {
		d.Parent.Logger.Error("admin.docs.edit.render", slog.String("err", err.Error()))
		http.Error(w, "internal", http.StatusInternalServerError)
	}
}

// --- Save (new or update) --------------------------------------------------

// SaveDoc handles POST /manage/docs/new and POST /manage/docs/:slug/edit.
func (d *DocHandlers) SaveDoc(w http.ResponseWriter, r *http.Request) {
	sess, ok := d.Parent.Auth.ParseSession(r)
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

	isNew := strings.HasSuffix(r.URL.Path, "/new")
	body := r.Form.Get("body")
	if strings.TrimSpace(body) == "" {
		d.editorError(w, r, isNew, "", body, "正文不能为空")
		return
	}

	// Parse the Markdown to extract/validate slug via content package helpers.
	slug, err := extractSlugFromBody(body)
	if err != nil {
		d.editorError(w, r, isNew, "", body, "frontmatter 错误："+err.Error())
		return
	}

	// Existence & conflict checks.
	existingSlug := ""
	if !isNew {
		existingSlug = extractSlug(r.URL.Path, "/manage/docs/", "/edit")
		if existingSlug == "" {
			http.NotFound(w, r)
			return
		}
	}
	if isNew {
		if _, clash := d.Content.Docs().Get(content.KindDoc, slug); clash {
			d.editorError(w, r, isNew, slug, body, "slug "+slug+" 已存在")
			return
		}
	} else if slug != existingSlug {
		// Slug change requires rename — refuse for now, ask to delete + recreate.
		d.editorError(w, r, isNew, existingSlug, body, "修改 slug 请先删除旧条目再新建")
		return
	}

	targetPath := filepath.Join(d.DataDir, "content", "docs", slug+".md")
	if err := storage.AtomicWrite(targetPath, []byte(body), 0o644); err != nil {
		d.Parent.Logger.Error("admin.docs.save", slog.String("err", err.Error()))
		d.editorError(w, r, isNew, slug, body, "保存失败："+err.Error())
		return
	}
	// Trigger content reload so the new entry appears immediately.
	if err := d.Content.Reload(); err != nil {
		d.Parent.Logger.Warn("admin.docs.save.reload", slog.String("err", err.Error()))
	}
	http.Redirect(w, r, "/manage/docs", http.StatusSeeOther)
}

func (d *DocHandlers) editorError(w http.ResponseWriter, r *http.Request, isNew bool, slug, body, msg string) {
	sess, _ := d.Parent.Auth.ParseSession(r)
	data := docEditData{IsNew: isNew, Slug: slug, Body: body, CSRF: sess.CSRF, Error: msg, Kind: "doc"}
	w.WriteHeader(http.StatusBadRequest)
	_ = d.Parent.Tpl.Render(w, r, http.StatusBadRequest, "admin_doc_edit.html", data)
}

// --- Delete ---------------------------------------------------------------

// DeleteDoc handles POST /manage/docs/:slug/delete.
func (d *DocHandlers) DeleteDoc(w http.ResponseWriter, r *http.Request) {
	sess, ok := d.Parent.Auth.ParseSession(r)
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
	slug := extractSlug(r.URL.Path, "/manage/docs/", "/delete")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	e, ok := d.Content.Docs().Get(content.KindDoc, slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	trashDir := filepath.Join(d.DataDir, "trash")
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		d.Parent.Logger.Error("admin.docs.delete.mkdir", slog.String("err", err.Error()))
		http.Error(w, "trash mkdir failed", http.StatusInternalServerError)
		return
	}
	trashName := time.Now().UTC().Format("20060102-150405") + "-" + slug + ".md"
	target := filepath.Join(trashDir, trashName)
	if err := os.Rename(e.Path, target); err != nil {
		d.Parent.Logger.Error("admin.docs.delete.rename", slog.String("err", err.Error()))
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if err := d.Content.Reload(); err != nil {
		d.Parent.Logger.Warn("admin.docs.delete.reload", slog.String("err", err.Error()))
	}
	http.Redirect(w, r, "/manage/docs", http.StatusSeeOther)
}

// --- Helpers ---------------------------------------------------------------

// extractSlug pulls the slug from `/manage/{kind}/<slug>{suffix}`.
func extractSlug(path, prefix, suffix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := path[len(prefix):]
	if suffix != "" {
		if !strings.HasSuffix(rest, suffix) {
			return ""
		}
		rest = rest[:len(rest)-len(suffix)]
	}
	if strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

func defaultDocFrontmatter() string {
	today := time.Now().UTC().Format("2006-01-02")
	return "---\n" +
		"title: \n" +
		"slug: \n" +
		"tags: []\n" +
		"category: \n" +
		"created: " + today + "\n" +
		"updated: " + today + "\n" +
		"status: draft\n" +
		"featured: false\n" +
		"---\n\n" +
		"正文从这里开始。\n"
}

// extractSlugFromBody scans the YAML frontmatter of the submitted MD for the
// slug value and returns an error if any critical field is missing.
func extractSlugFromBody(body string) (string, error) {
	// Very lightweight parse: find `---\n...\n---\n` block and look for
	// `slug:` line. A full parse happens after the file is written, on the
	// next content.Reload. This keeps the admin-edit path fast for common
	// cases while still blocking obviously malformed submissions.
	s := strings.TrimLeft(body, " \t\r\n")
	if !strings.HasPrefix(s, "---") {
		return "", fmt.Errorf("文件开头缺少 frontmatter 起始 `---`")
	}
	newl := strings.Index(s, "\n")
	if newl < 0 {
		return "", fmt.Errorf("frontmatter 未闭合")
	}
	rest := s[newl+1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", fmt.Errorf("frontmatter 未闭合 (缺少结尾 ---)")
	}
	fm := rest[:end]
	for _, line := range strings.Split(fm, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "slug:") {
			val := strings.TrimSpace(strings.TrimPrefix(trim, "slug:"))
			val = strings.Trim(val, "\"'")
			if val == "" {
				return "", fmt.Errorf("slug 不能为空")
			}
			if !isSafeSlug(val) {
				return "", fmt.Errorf("slug 仅支持小写字母、数字、短横线")
			}
			return val, nil
		}
	}
	return "", fmt.Errorf("缺少 slug 字段")
}

func isSafeSlug(s string) bool {
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
	return s[0] != '-' && s[len(s)-1] != '-'
}

// URLEscape is exposed for templates that redirect with query params.
func URLEscape(s string) string { return url.QueryEscape(s) }
