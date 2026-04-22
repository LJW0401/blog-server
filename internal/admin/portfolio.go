package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/storage"
)

// PortfolioHandlers manages CRUD for portfolio entries. Files live under
// $DataDir/content/portfolio/. Soft-delete moves files into
// $DataDir/trash/portfolio/ via the shared trash conventions (see trash.go).
//
// Editor template and preview endpoint are shared with DocHandlers —
// admin_doc_edit.html renders correctly for Kind="portfolio" because all
// portfolio-specific fields are just frontmatter lines.
type PortfolioHandlers struct {
	Parent  *Handlers
	Content *content.Store
	DataDir string
}

// --- List -----------------------------------------------------------------

// List handles GET /manage/portfolio.
func (p *PortfolioHandlers) List(w http.ResponseWriter, r *http.Request) {
	sess, _ := p.Parent.Auth.ParseSession(r)
	entries := p.Content.Portfolios().List(content.KindPortfolio)
	featured := make([]*content.Entry, 0, len(entries))
	others := make([]*content.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Featured {
			featured = append(featured, e)
		} else {
			others = append(others, e)
		}
	}
	// featured: Order ASC, Updated DESC — matches homepage display order.
	sort.SliceStable(featured, func(i, j int) bool {
		if featured[i].Order != featured[j].Order {
			return featured[i].Order < featured[j].Order
		}
		return featured[i].Updated.After(featured[j].Updated)
	})
	// others: Updated DESC — order field isn't user-facing for non-featured.
	sort.SliceStable(others, func(i, j int) bool {
		return others[i].Updated.After(others[j].Updated)
	})
	data := map[string]any{
		"Entries":  entries,
		"Featured": featured,
		"Others":   others,
		"Stats":    portfolioStats(p.Content),
		"CSRF":     sess.CSRF,
		"Info":     r.URL.Query().Get("info"),
		"Error":    r.URL.Query().Get("e"),
	}
	if err := p.Parent.Tpl.Render(w, r, http.StatusOK, "admin_portfolio_list.html", data); err != nil {
		p.Parent.Logger.Error("admin.portfolio.list", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// --- Editor ---------------------------------------------------------------

// New handles GET /manage/portfolio/new.
func (p *PortfolioHandlers) New(w http.ResponseWriter, r *http.Request) {
	sess, _ := p.Parent.Auth.ParseSession(r)
	data := docEditData{
		IsNew: true,
		Slug:  "",
		Body:  defaultPortfolioFrontmatter(),
		CSRF:  sess.CSRF,
		Kind:  "portfolio",
	}
	p.renderEditor(w, r, data)
}

// Edit handles GET /manage/portfolio/:slug/edit.
func (p *PortfolioHandlers) Edit(w http.ResponseWriter, r *http.Request) {
	slug := extractSlug(r.URL.Path, "/manage/portfolio/", "/edit")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	e, ok := p.Content.Portfolios().Get(content.KindPortfolio, slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	raw, err := os.ReadFile(e.Path)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	sess, _ := p.Parent.Auth.ParseSession(r)
	data := docEditData{
		IsNew: false,
		Slug:  slug,
		Body:  string(raw),
		CSRF:  sess.CSRF,
		Kind:  "portfolio",
	}
	p.renderEditor(w, r, data)
}

func (p *PortfolioHandlers) renderEditor(w http.ResponseWriter, r *http.Request, data docEditData) {
	if err := p.Parent.Tpl.Render(w, r, http.StatusOK, "admin_doc_edit.html", data); err != nil {
		p.Parent.Logger.Error("admin.portfolio.edit.render", slog.String("err", err.Error()))
		http.Error(w, "internal", http.StatusInternalServerError)
	}
}

func (p *PortfolioHandlers) editorError(w http.ResponseWriter, r *http.Request, isNew bool, slug, body, msg string) {
	sess, _ := p.Parent.Auth.ParseSession(r)
	data := docEditData{
		IsNew: isNew, Slug: slug, Body: body, CSRF: sess.CSRF, Error: msg, Kind: "portfolio",
	}
	_ = p.Parent.Tpl.Render(w, r, http.StatusBadRequest, "admin_doc_edit.html", data)
}

// --- Save (new or update) -------------------------------------------------

// Save handles POST /manage/portfolio/new and POST /manage/portfolio/:slug/edit.
func (p *PortfolioHandlers) Save(w http.ResponseWriter, r *http.Request) {
	sess, ok := p.Parent.Auth.ParseSession(r)
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
		p.editorError(w, r, isNew, "", body, "正文不能为空")
		return
	}

	slug, err := extractSlugFromBody(body)
	if err != nil {
		p.editorError(w, r, isNew, "", body, "frontmatter 错误："+err.Error())
		return
	}

	existingSlug := ""
	if !isNew {
		existingSlug = extractSlug(r.URL.Path, "/manage/portfolio/", "/edit")
		if existingSlug == "" {
			http.NotFound(w, r)
			return
		}
	}
	if isNew {
		if _, clash := p.Content.Portfolios().Get(content.KindPortfolio, slug); clash {
			p.editorError(w, r, isNew, slug, body, "slug "+slug+" 已存在")
			return
		}
	} else if slug != existingSlug {
		p.editorError(w, r, isNew, existingSlug, body, "修改 slug 请先删除旧条目再新建")
		return
	}

	targetPath := filepath.Join(p.DataDir, "content", "portfolio", slug+".md")
	if err := storage.AtomicWrite(targetPath, []byte(body), 0o644); err != nil {
		p.Parent.Logger.Error("admin.portfolio.save", slog.String("err", err.Error()))
		p.editorError(w, r, isNew, slug, body, "保存失败："+err.Error())
		return
	}
	if err := p.Content.Reload(); err != nil {
		p.Parent.Logger.Warn("admin.portfolio.save.reload", slog.String("err", err.Error()))
	}
	http.Redirect(w, r, "/manage/portfolio", http.StatusSeeOther)
}

// --- Delete ---------------------------------------------------------------

// Delete handles POST /manage/portfolio/:slug/delete.
func (p *PortfolioHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	sess, ok := p.Parent.Auth.ParseSession(r)
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
	slug := extractSlug(r.URL.Path, "/manage/portfolio/", "/delete")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	e, ok := p.Content.Portfolios().Get(content.KindPortfolio, slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	trashDir := filepath.Join(p.DataDir, "trash", TrashKindPortfolio)
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		p.Parent.Logger.Error("admin.portfolio.delete.mkdir", slog.String("err", err.Error()))
		http.Error(w, "trash mkdir failed", http.StatusInternalServerError)
		return
	}
	trashName := time.Now().UTC().Format("20060102-150405") + "-" + slug + ".md"
	target := filepath.Join(trashDir, trashName)
	if err := os.Rename(e.Path, target); err != nil {
		p.Parent.Logger.Error("admin.portfolio.delete.rename", slog.String("err", err.Error()))
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	if err := p.Content.Reload(); err != nil {
		p.Parent.Logger.Warn("admin.portfolio.delete.reload", slog.String("err", err.Error()))
	}
	http.Redirect(w, r, "/manage/portfolio", http.StatusSeeOther)
}

// ToggleFeatured handles POST /manage/portfolio/:slug/featured. Body: csrf,
// featured ("true"/"false"). Rewrites the YAML frontmatter in place.
func (p *PortfolioHandlers) ToggleFeatured(w http.ResponseWriter, r *http.Request) {
	sess, ok := p.Parent.Auth.ParseSession(r)
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
	slug := extractSlug(r.URL.Path, "/manage/portfolio/", "/featured")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	e, ok := p.Content.Portfolios().Get(content.KindPortfolio, slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	wantFeatured := r.Form.Get("featured") == "true"
	raw, err := os.ReadFile(e.Path)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	updated, err := setFrontmatterField(string(raw), "featured", boolYAML(wantFeatured))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 从非置顶切到置顶时，把 order 设为"现有置顶 max + 10"，让新上榜的
	// 作品落在主页末尾，避免沿用原值（常见是默认的 0）导致意外插队。
	// 转换为非置顶、或原本就已置顶（幂等重放）时不动 order。
	if wantFeatured && !e.Featured {
		maxOrd := 0
		for _, fe := range p.Content.Portfolios().List(content.KindPortfolio) {
			if fe.Featured && fe.Slug != slug && fe.Order > maxOrd {
				maxOrd = fe.Order
			}
		}
		updated, err = setFrontmatterField(updated, "order", strconv.Itoa(maxOrd+10))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := storage.AtomicWrite(e.Path, []byte(updated), 0o644); err != nil {
		p.Parent.Logger.Error("admin.portfolio.featured.write", slog.String("err", err.Error()))
		http.Error(w, "write failed", http.StatusInternalServerError)
		return
	}
	if err := p.Content.Reload(); err != nil {
		p.Parent.Logger.Warn("admin.portfolio.featured.reload", slog.String("err", err.Error()))
	}
	http.Redirect(w, r, "/manage/portfolio", http.StatusSeeOther)
}

// UpdateOrder handles POST /manage/portfolio/:slug/order.
// Body: csrf, order (integer 0..9999). Returns JSON {ok: bool, error?: str}.
// Rewrites only the frontmatter `order` field, keeping the rest of the file
// byte-identical to the previous on-disk copy.
func (p *PortfolioHandlers) UpdateOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	sess, ok := p.Parent.Auth.ParseSession(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"error":"未登录"}`))
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "bad form")
		return
	}
	if !auth.CSRFValid(sess, r.Form.Get("csrf")) {
		writeJSONErr(w, http.StatusForbidden, "csrf")
		return
	}
	slug := extractSlug(r.URL.Path, "/manage/portfolio/", "/order")
	if slug == "" {
		writeJSONErr(w, http.StatusBadRequest, "slug 非法")
		return
	}
	e, ok := p.Content.Portfolios().Get(content.KindPortfolio, slug)
	if !ok {
		writeJSONErr(w, http.StatusNotFound, "作品不存在")
		return
	}
	rawOrder := strings.TrimSpace(r.Form.Get("order"))
	n, err := strconv.Atoi(rawOrder)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "order 必须为整数")
		return
	}
	if n < 0 || n > 9999 {
		writeJSONErr(w, http.StatusBadRequest, "order 必须为 0~9999 的整数")
		return
	}
	raw, err := os.ReadFile(e.Path)
	if err != nil {
		p.Parent.Logger.Error("admin.portfolio.order.read", slog.String("err", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "read failed")
		return
	}
	updated, err := setFrontmatterField(string(raw), "order", strconv.Itoa(n))
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := storage.AtomicWrite(e.Path, []byte(updated), 0o644); err != nil {
		p.Parent.Logger.Error("admin.portfolio.order.write", slog.String("err", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "write failed")
		return
	}
	if err := p.Content.Reload(); err != nil {
		p.Parent.Logger.Warn("admin.portfolio.order.reload", slog.String("err", err.Error()))
	}
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	b, _ := json.Marshal(map[string]any{"ok": false, "error": msg})
	_, _ = w.Write(b)
}

// --- Helpers --------------------------------------------------------------

func defaultPortfolioFrontmatter() string {
	today := time.Now().UTC().Format("2006-01-02")
	return "---\n" +
		"title: \n" +
		"slug: \n" +
		"description: \n" +
		"category: \n" +
		"tags: []\n" +
		"cover: \n" +
		"order: 0\n" +
		"demo_url: \n" +
		"source_url: \n" +
		"created: " + today + "\n" +
		"updated: " + today + "\n" +
		"status: draft\n" +
		"featured: false\n" +
		"---\n\n" +
		"<!-- portfolio:intro -->\n" +
		"主页卡片展示的长简介（支持 Markdown）。\n" +
		"<!-- /portfolio:intro -->\n\n" +
		"# 详情页正文\n\n" +
		"从这里开始写。\n"
}

func boolYAML(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// setFrontmatterField rewrites a single `key: value` line inside the leading
// YAML frontmatter block. The key must already exist; if absent returns an
// error (callers shouldn't be inventing new fields on mutation endpoints).
// Preserves all other lines, indentation and body bytes exactly.
//
// 假设：frontmatter 为扁平结构（无嵌套对象）。匹配首个 `trim(line)` 以
// `key:` 开头的行，因此如果将来引入嵌套 YAML 且子对象包含同名 key，会
// 改错位置。当前 portfolio/doc/project schema 均扁平，安全。
func setFrontmatterField(body, key, value string) (string, error) {
	s := body
	if !strings.HasPrefix(strings.TrimLeft(s, " \t\r\n"), "---") {
		return "", newFMError("frontmatter missing")
	}
	nl := strings.Index(s, "\n")
	if nl < 0 {
		return "", newFMError("frontmatter not closed")
	}
	rest := s[nl+1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", newFMError("frontmatter not closed")
	}
	fm := rest[:end]
	tail := rest[end:] // includes leading `\n---...`
	var out []string
	found := false
	for _, line := range strings.Split(fm, "\n") {
		trim := strings.TrimSpace(line)
		if !found && strings.HasPrefix(trim, key+":") {
			out = append(out, key+": "+value)
			found = true
			continue
		}
		out = append(out, line)
	}
	if !found {
		return "", newFMError("field " + key + " not found")
	}
	return s[:nl+1] + strings.Join(out, "\n") + tail, nil
}

type fmError struct{ msg string }

func (e *fmError) Error() string  { return "frontmatter: " + e.msg }
func newFMError(msg string) error { return &fmError{msg: msg} }
