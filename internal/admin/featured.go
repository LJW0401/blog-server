// 文档/项目"主页展示"开关：复用 portfolio 已经验证过的
// setFrontmatterField 写盘流程，把开关状态映射到 entry 的 featured 字段。
// 仅做 frontmatter 改写、文件回写、内容仓重建三步；与 portfolio 的差异
// 在于 docs/projects 没有 order 字段，主页排序沿用 content 仓自带的
// updated 倒序。
package admin

import (
	"net/http"
	"os"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/storage"
)

// toggleFeaturedFile 在指定 path 对应的 MD 文件里把 featured 改为 want；
// 写完调 reload 让 content 仓刷新。任一步失败返回非 nil error，调用方
// 自己回 5xx，避免在这里直接污染 ResponseWriter。
func toggleFeaturedFile(path string, want bool, reload func() error) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, err := setFrontmatterField(string(raw), "featured", boolYAML(want))
	if err != nil {
		return err
	}
	if err := storage.AtomicWrite(path, []byte(updated), 0o644); err != nil {
		return err
	}
	if reload != nil {
		_ = reload()
	}
	return nil
}

// ToggleFeatured handles POST /manage/docs/:slug/featured.
// Form: csrf, featured("true"/"false")。重写文件中的 featured 字段后跳回列表。
func (d *DocHandlers) ToggleFeatured(w http.ResponseWriter, r *http.Request) {
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
	slug := extractSlug(r.URL.Path, "/manage/docs/", "/featured")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	e, ok := d.Content.Docs().Get(content.KindDoc, slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	want := r.Form.Get("featured") == "true"
	if err := toggleFeaturedFile(e.Path, want, d.Content.Reload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 不要带 #row-<slug> anchor：列表后段的行被滚到视口顶端时浏览器会把
	// 整页推到文档底部（"跳到底部"现象）。原地保留 scroll 由前端 JS fetch
	// 拦截负责；这里的回退路径只保证返回管理页本身。
	http.Redirect(w, r, "/manage/docs", http.StatusSeeOther)
}

// ToggleFeatured handles POST /manage/projects/:slug/featured.
func (ph *ProjectHandlers) ToggleFeatured(w http.ResponseWriter, r *http.Request) {
	sess, ok := ph.Parent.Auth.ParseSession(r)
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
	slug := extractSlug(r.URL.Path, "/manage/projects/", "/featured")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	e, ok := ph.Content.Projects().Get(content.KindProject, slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	want := r.Form.Get("featured") == "true"
	if err := toggleFeaturedFile(e.Path, want, ph.Content.Reload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/manage/repos", http.StatusSeeOther)
}
