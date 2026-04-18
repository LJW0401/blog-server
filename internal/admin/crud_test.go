package admin_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/admin"
	"github.com/penguin/blog-server/internal/assets"
	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/config"
	"github.com/penguin/blog-server/internal/content"
	gh "github.com/penguin/blog-server/internal/github"
	"github.com/penguin/blog-server/internal/render"
	"github.com/penguin/blog-server/internal/settings"
	"github.com/penguin/blog-server/internal/storage"
)

// crudSetup builds a fully-wired admin handler graph for end-to-end tests
// of docs/images/settings/projects CRUD.
type crudBundle struct {
	Admin    *admin.Handlers
	Docs     *admin.DocHandlers
	Images   *admin.ImageHandlers
	Settings *admin.SettingsHandlers
	Projects *admin.ProjectHandlers
	Content  *content.Store
	DataDir  string
	Cookie   *http.Cookie
	CSRF     string
}

func crudSetup(t *testing.T) *crudBundle {
	t.Helper()
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "content", "docs"), 0o700)
	_ = os.MkdirAll(filepath.Join(dir, "content", "projects"), 0o700)

	cfgPath := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfgPath, []byte("listen_addr: 127.0.0.1:8080\nadmin_username: admin\nadmin_password_bcrypt: \""+defaultPasswordHash+"\"\ndata_dir: \""+dir+"\"\ngithub_sync_interval_min: 30\n"), 0o600)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := storage.Open(dir)
	if err != nil && !errors.Is(err, storage.ErrCorruptDB) {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	secret, _ := auth.LoadOrCreateSecret(st.DB)
	authStore := auth.NewStore(st.DB, secret)
	tpl, err := render.NewTemplates(assets.Templates(), render.NewMarkdown())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adminH := admin.New(authStore, cfg, cfgPath, tpl, logger)

	cstore := content.New(dir, logger)
	if err := cstore.Reload(); err != nil {
		t.Fatal(err)
	}

	stStore := settings.New(st.DB)
	ghCache := gh.NewCache(st.DB)

	b := &crudBundle{
		Admin:    adminH,
		Docs:     &admin.DocHandlers{Parent: adminH, Content: cstore, DataDir: dir},
		Images:   &admin.ImageHandlers{Parent: adminH, DataDir: dir},
		Settings: &admin.SettingsHandlers{Parent: adminH, Settings: stStore},
		Projects: &admin.ProjectHandlers{
			Parent: adminH, Content: cstore, DataDir: dir,
			GitHubClient: nil, // many tests don't need GitHub; set per case
			GitHubCache:  ghCache,
		},
		Content: cstore,
		DataDir: dir,
	}

	// Log in to obtain a cookie + CSRF.
	form := url.Values{"username": {"admin"}, "password": {"supersecret"}}
	req := httptest.NewRequest("POST", "/manage/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.RemoteAddr = "203.0.113.5:12345"
	w := httptest.NewRecorder()
	adminH.LoginSubmit(w, req)
	for _, c := range w.Result().Cookies() {
		if c.Name == "blog_sess" {
			b.Cookie = c
			break
		}
	}
	if b.Cookie == nil {
		t.Fatal("login cookie missing")
	}
	// Parse session to get CSRF.
	req2 := httptest.NewRequest("GET", "/manage", nil)
	req2.AddCookie(b.Cookie)
	req2.Header.Set("User-Agent", "test/ua")
	sess, _ := adminH.SessionFromRequest(req2)
	b.CSRF = sess.CSRF
	return b
}

func (b *crudBundle) authedPost(t *testing.T, path string, form url.Values, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(b.Cookie)
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func (b *crudBundle) authedGet(t *testing.T, path string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(b.Cookie)
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

// --- Docs (WI-5.1, 5.3, 5.4, 5.7, 5.8) ------------------------------------

func TestDocsList_Smoke(t *testing.T) {
	b := crudSetup(t)
	w := b.authedGet(t, "/manage/docs", b.Docs.DocsList)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "文档管理") {
		t.Errorf("title missing")
	}
	if !strings.Contains(w.Body.String(), "新建文档") {
		t.Error("new button missing")
	}
}

func TestDocsEdit_Smoke_NewAndSave(t *testing.T) {
	b := crudSetup(t)
	mdBody := `---
title: 测试文档
slug: test-doc
status: published
updated: 2026-04-18
---

# Hello

正文。
`
	form := url.Values{"csrf": {b.CSRF}, "body": {mdBody}}
	w := b.authedPost(t, "/manage/docs/new", form, b.Docs.SaveDoc)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	// File should exist.
	path := filepath.Join(b.DataDir, "content", "docs", "test-doc.md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("doc file not created: %v", err)
	}
	// Index should include it after reload triggered by handler.
	if e, ok := b.Content.Docs().Get(content.KindDoc, "test-doc"); !ok || e.Title != "测试文档" {
		t.Errorf("index update failed: %+v", e)
	}
}

func TestDocsEdit_Edge_DuplicateSlugRejected(t *testing.T) {
	b := crudSetup(t)
	md := "---\ntitle: A\nslug: clash\nupdated: 2026-04-18\nstatus: published\n---\nbody\n"
	_ = b.authedPost(t, "/manage/docs/new", url.Values{"csrf": {b.CSRF}, "body": {md}}, b.Docs.SaveDoc)
	w := b.authedPost(t, "/manage/docs/new", url.Values{"csrf": {b.CSRF}, "body": {md}}, b.Docs.SaveDoc)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d (expected 400)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "已存在") {
		t.Errorf("conflict msg missing")
	}
}

func TestDocsEdit_Edge_InvalidFrontmatter(t *testing.T) {
	b := crudSetup(t)
	w := b.authedPost(t, "/manage/docs/new", url.Values{"csrf": {b.CSRF}, "body": {"no frontmatter here"}}, b.Docs.SaveDoc)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d", w.Code)
	}
}

func TestDocsEdit_Edge_CSRFMissing(t *testing.T) {
	b := crudSetup(t)
	md := "---\ntitle: A\nslug: x\nupdated: 2026-04-18\nstatus: published\n---\nbody\n"
	w := b.authedPost(t, "/manage/docs/new", url.Values{"csrf": {""}, "body": {md}}, b.Docs.SaveDoc)
	if w.Code != http.StatusForbidden {
		t.Errorf("status %d", w.Code)
	}
}

func TestDocsDelete_Smoke(t *testing.T) {
	b := crudSetup(t)
	md := "---\ntitle: Del\nslug: delme\nupdated: 2026-04-18\nstatus: published\n---\nbody\n"
	_ = b.authedPost(t, "/manage/docs/new", url.Values{"csrf": {b.CSRF}, "body": {md}}, b.Docs.SaveDoc)
	// Confirm it's there.
	if _, ok := b.Content.Docs().Get(content.KindDoc, "delme"); !ok {
		t.Fatal("precondition: doc not created")
	}
	w := b.authedPost(t, "/manage/docs/delme/delete", url.Values{"csrf": {b.CSRF}}, b.Docs.DeleteDoc)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if _, ok := b.Content.Docs().Get(content.KindDoc, "delme"); ok {
		t.Error("doc should be removed from index")
	}
	// trash/ should have a dated file.
	entries, _ := os.ReadDir(filepath.Join(b.DataDir, "trash"))
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "delme") {
			found = true
		}
	}
	if !found {
		t.Error("trash file not found")
	}
}

func TestDocsDelete_Edge_Unknown(t *testing.T) {
	b := crudSetup(t)
	w := b.authedPost(t, "/manage/docs/nope/delete", url.Values{"csrf": {b.CSRF}}, b.Docs.DeleteDoc)
	if w.Code != http.StatusNotFound {
		t.Errorf("status %d", w.Code)
	}
}

// --- Images (WI-5.10, 5.11) ----------------------------------------------

// buildMultipart returns (body, boundary) for a multipart form with csrf and
// an image file.
func buildMultipart(t *testing.T, csrf string, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("csrf", csrf)
	w, _ := mw.CreateFormFile("image", filename)
	_, _ = w.Write(content)
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

// 1x1 transparent PNG.
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

func TestImages_Smoke_UploadPNG(t *testing.T) {
	b := crudSetup(t)
	body, ct := buildMultipart(t, b.CSRF, "one.png", tinyPNG)
	req := httptest.NewRequest("POST", "/manage/images/upload", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(b.Cookie)
	req.Header.Set("User-Agent", "test/ua")
	w := httptest.NewRecorder()
	b.Images.ImagesUpload(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	entries, _ := os.ReadDir(filepath.Join(b.DataDir, "images"))
	if len(entries) != 1 {
		t.Errorf("expected 1 image, got %d", len(entries))
	}
}

func TestImages_Smoke_DuplicateContentReused(t *testing.T) {
	b := crudSetup(t)
	for i := 0; i < 2; i++ {
		body, ct := buildMultipart(t, b.CSRF, "dup.png", tinyPNG)
		req := httptest.NewRequest("POST", "/manage/images/upload", body)
		req.Header.Set("Content-Type", ct)
		req.AddCookie(b.Cookie)
		req.Header.Set("User-Agent", "test/ua")
		b.Images.ImagesUpload(httptest.NewRecorder(), req)
	}
	entries, _ := os.ReadDir(filepath.Join(b.DataDir, "images"))
	if len(entries) != 1 {
		t.Errorf("dup upload should not create 2 files, got %d", len(entries))
	}
}

func TestImages_Edge_TooLarge(t *testing.T) {
	b := crudSetup(t)
	big := bytes.Repeat([]byte("x"), 6*1024*1024)
	body, ct := buildMultipart(t, b.CSRF, "big.png", big)
	req := httptest.NewRequest("POST", "/manage/images/upload", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(b.Cookie)
	req.Header.Set("User-Agent", "test/ua")
	w := httptest.NewRecorder()
	b.Images.ImagesUpload(w, req)
	if w.Code != http.StatusSeeOther && w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d (expected 303 w/ error or 413)", w.Code)
	}
}

func TestImages_Edge_WrongMIME(t *testing.T) {
	b := crudSetup(t)
	txt := []byte("hello world not an image")
	body, ct := buildMultipart(t, b.CSRF, "evil.txt", txt)
	req := httptest.NewRequest("POST", "/manage/images/upload", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(b.Cookie)
	req.Header.Set("User-Agent", "test/ua")
	w := httptest.NewRecorder()
	b.Images.ImagesUpload(w, req)
	if w.Code != http.StatusSeeOther && w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status %d", w.Code)
	}
}

func TestImages_Edge_CSRFMissing(t *testing.T) {
	b := crudSetup(t)
	body, ct := buildMultipart(t, "", "x.png", tinyPNG)
	req := httptest.NewRequest("POST", "/manage/images/upload", body)
	req.Header.Set("Content-Type", ct)
	req.AddCookie(b.Cookie)
	req.Header.Set("User-Agent", "test/ua")
	w := httptest.NewRecorder()
	b.Images.ImagesUpload(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status %d", w.Code)
	}
}

// --- Settings (WI-5.14, 5.15) ---------------------------------------------

func TestSettings_Smoke_SaveAndRead(t *testing.T) {
	b := crudSetup(t)
	form := url.Values{
		"csrf":              {b.CSRF},
		"name":              {"Penguin"},
		"tagline":           {"我的 tagline"},
		"location":          {"中国"},
		"direction":         {"后端"},
		"status":            {"活跃"},
		"qq_group":          {"772436864"},
		"media_bilibili":    {"https://b.example/user"},
		"media_douyin":      {"https://d.example/user"},
		"media_xiaohongshu": {""},
	}
	w := b.authedPost(t, "/manage/settings", form, b.Settings.SettingsSubmit)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	// Subsequent GET should reflect.
	g := b.authedGet(t, "/manage/settings", b.Settings.SettingsPage)
	body := g.Body.String()
	if !strings.Contains(body, "我的 tagline") {
		t.Error("tagline not saved")
	}
	if !strings.Contains(body, "772436864") {
		t.Error("qq_group not saved")
	}
}

func TestSettings_Edge_TaglineRequired(t *testing.T) {
	b := crudSetup(t)
	form := url.Values{"csrf": {b.CSRF}, "tagline": {""}}
	w := b.authedPost(t, "/manage/settings", form, b.Settings.SettingsSubmit)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "e=") {
		t.Errorf("expected error redirect: %s", loc)
	}
}

func TestSettings_Edge_QQDigitsOnly(t *testing.T) {
	b := crudSetup(t)
	form := url.Values{"csrf": {b.CSRF}, "tagline": {"x"}, "qq_group": {"abc123"}}
	w := b.authedPost(t, "/manage/settings", form, b.Settings.SettingsSubmit)
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "e=") {
		t.Errorf("expected error: %s", loc)
	}
}

func TestSettings_Edge_URLMustHavePrefix(t *testing.T) {
	b := crudSetup(t)
	form := url.Values{"csrf": {b.CSRF}, "tagline": {"x"}, "media_bilibili": {"b.example.com"}}
	w := b.authedPost(t, "/manage/settings", form, b.Settings.SettingsSubmit)
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "e=") {
		t.Errorf("expected error: %s", loc)
	}
}

// --- Projects (WI-5.18, 5.19) ---------------------------------------------

func withMockGitHub(t *testing.T, status int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		_, _ = io.WriteString(w, `{"full_name":"foo/bar","stargazers_count":1,"html_url":"https://example","pushed_at":"2026-04-15T00:00:00Z","updated_at":"2026-04-15T00:00:00Z"}`)
	})
	return httptest.NewServer(mux)
}

func TestProjects_Smoke_NewWithValidRepo(t *testing.T) {
	b := crudSetup(t)
	srv := withMockGitHub(t, 200)
	t.Cleanup(srv.Close)
	b.Projects.GitHubClient = gh.NewClient(gh.WithBaseURL(srv.URL))
	form := url.Values{
		"csrf":         {b.CSRF},
		"repo":         {"foo/bar"},
		"slug":         {"bar"},
		"display_name": {"Bar Project"},
		"category":     {"工具"},
	}
	w := b.authedPost(t, "/manage/repos/new", form, b.Projects.ReposNew)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if _, ok := b.Content.Projects().Get(content.KindProject, "bar"); !ok {
		t.Error("project not indexed after register")
	}
}

func TestProjects_Edge_GitHubNotFound(t *testing.T) {
	b := crudSetup(t)
	srv := withMockGitHub(t, 404)
	t.Cleanup(srv.Close)
	b.Projects.GitHubClient = gh.NewClient(gh.WithBaseURL(srv.URL))
	form := url.Values{"csrf": {b.CSRF}, "repo": {"foo/bar"}, "slug": {"bar"}}
	w := b.authedPost(t, "/manage/repos/new", form, b.Projects.ReposNew)
	if w.Code != http.StatusSeeOther {
		t.Errorf("status %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Location"), "e=") {
		t.Errorf("expected error redirect")
	}
}

func TestProjects_Edge_DuplicateRepo(t *testing.T) {
	b := crudSetup(t)
	srv := withMockGitHub(t, 200)
	t.Cleanup(srv.Close)
	b.Projects.GitHubClient = gh.NewClient(gh.WithBaseURL(srv.URL))
	form := url.Values{"csrf": {b.CSRF}, "repo": {"foo/bar"}, "slug": {"bar"}}
	_ = b.authedPost(t, "/manage/repos/new", form, b.Projects.ReposNew)
	// Second attempt with same repo, different slug.
	form2 := url.Values{"csrf": {b.CSRF}, "repo": {"foo/bar"}, "slug": {"other"}}
	w := b.authedPost(t, "/manage/repos/new", form2, b.Projects.ReposNew)
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "e=") {
		t.Errorf("expected dup error: %s", loc)
	}
}

func TestProjects_Edge_Delete(t *testing.T) {
	b := crudSetup(t)
	srv := withMockGitHub(t, 200)
	t.Cleanup(srv.Close)
	b.Projects.GitHubClient = gh.NewClient(gh.WithBaseURL(srv.URL))
	_ = b.authedPost(t, "/manage/repos/new",
		url.Values{"csrf": {b.CSRF}, "repo": {"foo/bar"}, "slug": {"bar"}},
		b.Projects.ReposNew)
	// Populate cache to verify deletion cleans it.
	_ = b.Projects.GitHubCache.Upsert(context.Background(), gh.CacheEntry{Repo: "foo/bar", Info: &gh.RepoInfo{Stars: 1}})
	w := b.authedPost(t, "/manage/projects/bar/delete", url.Values{"csrf": {b.CSRF}}, b.Projects.ReposDelete)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if _, ok := b.Content.Projects().Get(content.KindProject, "bar"); ok {
		t.Error("should be removed from index")
	}
	if e, _ := b.Projects.GitHubCache.Get(context.Background(), "foo/bar"); e != nil {
		t.Error("cache row should be deleted")
	}
}
