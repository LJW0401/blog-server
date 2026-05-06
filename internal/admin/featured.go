// 文档/项目"主页展示"开关：复用 portfolio 已经验证过的 frontmatter 改写
// 流程，把开关状态映射到 entry 的 featured 字段。read-modify-write + reload；
// 与 portfolio 的差异在于 docs/projects 没有 order 字段，主页排序沿用 content
// 仓自带的 updated 倒序。
package admin

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/storage"
)

// toggleFeaturedFile 仅负责把 path 对应文件的 featured 字段写成 want。
// reload 留给 handler 层，handler 知道自己用哪个 logger 报警。
func toggleFeaturedFile(path string, want bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, err := setOrAddFrontmatterField(string(raw), "featured", boolYAML(want))
	if err != nil {
		return err
	}
	return storage.AtomicWrite(path, []byte(updated), 0o644)
}

// setOrAddFrontmatterField 改写既有 key 行；缺失时在 frontmatter 末尾追加。
// 用于"老手写 md 可能没填 featured 字段"的兼容写入：setFrontmatterField 把
// 缺字段当作硬错（avatar/settings 等端点期望字段必存），不能直接复用。
func setOrAddFrontmatterField(body, key, value string) (string, error) {
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
	tail := rest[end:]
	lines := strings.Split(fm, "\n")
	out := make([]string, 0, len(lines)+1)
	found := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if !found && strings.HasPrefix(trim, key+":") {
			out = append(out, key+": "+value)
			found = true
			continue
		}
		out = append(out, line)
	}
	if !found {
		out = append(out, key+": "+value)
	}
	return s[:nl+1] + strings.Join(out, "\n") + tail, nil
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
	if err := toggleFeaturedFile(e.Path, want); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := d.Content.Reload(); err != nil {
		d.Parent.Logger.Warn("admin.docs.featured.reload", slog.String("err", err.Error()))
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
	if err := toggleFeaturedFile(e.Path, want); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := ph.Content.Reload(); err != nil {
		ph.Parent.Logger.Warn("admin.projects.featured.reload", slog.String("err", err.Error()))
	}
	http.Redirect(w, r, "/manage/repos", http.StatusSeeOther)
}
