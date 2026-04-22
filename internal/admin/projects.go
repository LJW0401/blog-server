package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/content"
	gh "github.com/penguin/blog-server/internal/github"
	"github.com/penguin/blog-server/internal/storage"
)

// ProjectHandlers owns /manage/repos* and /manage/projects*.
type ProjectHandlers struct {
	Parent       *Handlers
	Content      *content.Store
	DataDir      string
	GitHubClient *gh.Client
	GitHubCache  *gh.Cache
}

// ReposList handles GET /manage/repos.
func (ph *ProjectHandlers) ReposList(w http.ResponseWriter, r *http.Request) {
	sess, _ := ph.Parent.Auth.ParseSession(r)
	entries := ph.Content.Projects().List(content.KindProject)
	data := map[string]any{
		"Projects": entries,
		"Stats":    projectStats(ph.Content),
		"CSRF":     sess.CSRF,
		"Error":    r.URL.Query().Get("e"),
		"Info":     r.URL.Query().Get("m"),
	}
	if err := ph.Parent.Tpl.Render(w, r, http.StatusOK, "admin_repos_list.html", data); err != nil {
		ph.Parent.Logger.Error("admin.repos.render", slog.String("err", err.Error()))
		http.Error(w, "internal", http.StatusInternalServerError)
	}
}

// ReposNew handles POST /manage/repos/new.
// Form: repo=<owner>/<name>, slug=<slug>, display_name?, category?
func (ph *ProjectHandlers) ReposNew(w http.ResponseWriter, r *http.Request) {
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
	repo := strings.TrimSpace(r.Form.Get("repo"))
	slug := strings.TrimSpace(r.Form.Get("slug"))
	owner, name, ok := splitOwnerName(repo)
	if !ok {
		redirectRepos(w, r, "repo 需为 owner/name 格式", "")
		return
	}
	if !isSafeSlug(slug) {
		redirectRepos(w, r, "slug 非法：仅小写字母/数字/短横线，不能首尾为 -", "")
		return
	}
	// Conflict checks.
	if _, dup := ph.Content.Projects().Get(content.KindProject, slug); dup {
		redirectRepos(w, r, "slug "+slug+" 已存在", "")
		return
	}
	for _, p := range ph.Content.Projects().List(content.KindProject) {
		if p.Repo == repo {
			redirectRepos(w, r, "仓库 "+repo+" 已登记", "")
			return
		}
	}

	// GitHub verification.
	if ph.GitHubClient != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		if _, err := ph.GitHubClient.GetRepo(ctx, owner, name, ""); err != nil {
			msg := mapGitHubError(err)
			redirectRepos(w, r, msg, "")
			return
		}
	}

	display := strings.TrimSpace(r.Form.Get("display_name"))
	if display == "" {
		display = name
	}
	category := strings.TrimSpace(r.Form.Get("category"))
	desc := strings.TrimSpace(r.Form.Get("display_desc"))

	today := time.Now().UTC().Format("2006-01-02")
	body := "---\n" +
		"slug: " + slug + "\n" +
		"repo: " + repo + "\n" +
		"display_name: " + display + "\n" +
		"display_desc: " + desc + "\n" +
		"category: " + category + "\n" +
		"stack: []\n" +
		"status: developing\n" +
		"featured: false\n" +
		"created: " + today + "\n" +
		"updated: " + today + "\n" +
		"---\n\n" +
		"这是项目 " + display + " 的本地介绍，登记后可在管理端编辑。\n"

	target := filepath.Join(ph.DataDir, "content", "projects", slug+".md")
	if err := storage.AtomicWrite(target, []byte(body), 0o644); err != nil {
		redirectRepos(w, r, "保存失败："+err.Error(), "")
		return
	}
	if err := ph.Content.Reload(); err != nil {
		ph.Parent.Logger.Warn("admin.repos.new.reload", slog.String("err", err.Error()))
	}
	redirectRepos(w, r, "", "已登记："+repo)
}

// ReposDelete handles POST /manage/projects/:slug/delete.
func (ph *ProjectHandlers) ReposDelete(w http.ResponseWriter, r *http.Request) {
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
	slug := extractSlug(r.URL.Path, "/manage/projects/", "/delete")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	e, ok := ph.Content.Projects().Get(content.KindProject, slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	trashDir := filepath.Join(ph.DataDir, "trash", TrashKindProject)
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		http.Error(w, "mkdir", http.StatusInternalServerError)
		return
	}
	target := filepath.Join(trashDir, time.Now().UTC().Format("20060102-150405")+"-"+slug+".md")
	if err := os.Rename(e.Path, target); err != nil {
		http.Error(w, "rename", http.StatusInternalServerError)
		return
	}
	// Clean the cache row too.
	if ph.GitHubCache != nil && e.Repo != "" {
		_ = ph.GitHubCache.Delete(r.Context(), e.Repo)
	}
	if err := ph.Content.Reload(); err != nil {
		ph.Parent.Logger.Warn("admin.repos.delete.reload", slog.String("err", err.Error()))
	}
	redirectRepos(w, r, "", "已删除：/projects/"+slug)
}

// EditProject handles GET /manage/projects/:slug/edit — reuses the shared
// doc editor template (Kind=project).
func (ph *ProjectHandlers) EditProject(w http.ResponseWriter, r *http.Request) {
	slug := extractSlug(r.URL.Path, "/manage/projects/", "/edit")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	e, ok := ph.Content.Projects().Get(content.KindProject, slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	raw, err := os.ReadFile(e.Path)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	sess, _ := ph.Parent.Auth.ParseSession(r)
	data := docEditData{IsNew: false, Slug: slug, Body: string(raw), CSRF: sess.CSRF, Kind: "project"}
	if err := ph.Parent.Tpl.Render(w, r, http.StatusOK, "admin_doc_edit.html", data); err != nil {
		ph.Parent.Logger.Error("admin.projects.edit.render", slog.String("err", err.Error()))
		http.Error(w, "internal", http.StatusInternalServerError)
	}
}

// SaveProject handles POST /manage/projects/:slug/edit.
func (ph *ProjectHandlers) SaveProject(w http.ResponseWriter, r *http.Request) {
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
	slug := extractSlug(r.URL.Path, "/manage/projects/", "/edit")
	if slug == "" {
		http.NotFound(w, r)
		return
	}
	body := r.Form.Get("body")
	if strings.TrimSpace(body) == "" {
		redirectRepos(w, r, "正文不能为空", "")
		return
	}
	bodySlug, err := extractSlugFromBody(body)
	if err != nil {
		redirectRepos(w, r, "frontmatter 错误："+err.Error(), "")
		return
	}
	if bodySlug != slug {
		redirectRepos(w, r, "修改 slug 请先删除旧条目再新建", "")
		return
	}
	target := filepath.Join(ph.DataDir, "content", "projects", slug+".md")
	if err := storage.AtomicWrite(target, []byte(body), 0o644); err != nil {
		redirectRepos(w, r, "保存失败："+err.Error(), "")
		return
	}
	if err := ph.Content.Reload(); err != nil {
		ph.Parent.Logger.Warn("admin.projects.save.reload", slog.String("err", err.Error()))
	}
	redirectRepos(w, r, "", "已保存：/projects/"+slug)
}

func redirectRepos(w http.ResponseWriter, r *http.Request, errMsg, info string) {
	target := "/manage/repos"
	if errMsg != "" {
		target += "?e=" + URLEscape(errMsg)
	} else if info != "" {
		target += "?m=" + URLEscape(info)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func splitOwnerName(repo string) (string, string, bool) {
	i := strings.IndexByte(repo, '/')
	if i <= 0 || i == len(repo)-1 {
		return "", "", false
	}
	return repo[:i], repo[i+1:], true
}

func mapGitHubError(err error) string {
	switch {
	case errors.Is(err, gh.ErrNotFound):
		return "GitHub 未找到此仓库"
	case errors.Is(err, gh.ErrUnauthorized):
		return "GitHub 认证失败（token 无效）"
	case errors.Is(err, gh.ErrRateLimited):
		return "GitHub API 限流中，请稍后重试"
	case errors.Is(err, gh.ErrUpstream), errors.Is(err, gh.ErrNetwork):
		return "无法校验仓库，请稍后重试"
	default:
		return fmt.Sprintf("校验失败：%v", err)
	}
}
