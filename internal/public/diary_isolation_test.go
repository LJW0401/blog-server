package public_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/assets"
	"github.com/penguin/blog-server/internal/content"
	"github.com/penguin/blog-server/internal/public"
	"github.com/penguin/blog-server/internal/render"
	"github.com/penguin/blog-server/internal/settings"
	"github.com/penguin/blog-server/internal/storage"
)

// WI-3.14 硬安全断言：日记内容**绝不**出现在任何公共路由的响应里。
// 这是需求 2.6.3 的底线：日记是私密内容，公共访问必须零泄露。
//
// 当 content.Store 未来意外把 diary 扫进索引、或者模板误用日记路径时，
// 该测试会立刻挂，守住回归。
func TestPublic_HardAssertion_DiaryContentNeverLeaks(t *testing.T) {
	const marker = "DIARY_LEAK_MARKER_xyz_13adbeef"

	// 独立构造测试环境，避免复用 setup() 时 dir 不透明
	dir := t.TempDir()

	// content.Store 约定扫 content/docs 与 content/projects，不扫 diary
	if err := os.MkdirAll(filepath.Join(dir, "content", "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "content", "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	diaryDir := filepath.Join(dir, "content", "diary")
	if err := os.MkdirAll(diaryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// 写 marker 日记 —— 如果公共路由泄露它，测试会挂
	if err := os.WriteFile(filepath.Join(diaryDir, "2026-04-19.md"),
		[]byte("---\ndate: 2026-04-19\n---\n"+marker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cstore := content.New(dir, logger)
	if err := cstore.Reload(); err != nil {
		t.Fatal(err)
	}
	tpl, err := render.NewTemplates(assets.Templates(), render.NewMarkdown())
	if err != nil {
		t.Fatal(err)
	}
	st, err := storage.Open(dir)
	if err != nil && !errors.Is(err, storage.ErrCorruptDB) {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := public.NewHandlers(cstore, tpl, logger)
	h.SettingsDB = settings.New(st.DB)

	routes := []struct {
		name    string
		path    string
		handler func(w *httptest.ResponseRecorder, req *httptest.ResponseRecorder)
	}{}
	_ = routes // placeholder below uses inline switch

	cases := []struct {
		name string
		path string
		call func(rr *httptest.ResponseRecorder)
	}{
		{
			name: "home",
			path: "/",
			call: func(rr *httptest.ResponseRecorder) {
				req := httptest.NewRequest("GET", "/", nil)
				h.Home(rr, req)
			},
		},
		{
			name: "docs_list",
			path: "/docs",
			call: func(rr *httptest.ResponseRecorder) {
				req := httptest.NewRequest("GET", "/docs", nil)
				h.DocsList(rr, req)
			},
		},
		{
			name: "doc_detail_fake_slug",
			path: "/docs/2026-04-19",
			call: func(rr *httptest.ResponseRecorder) {
				req := httptest.NewRequest("GET", "/docs/2026-04-19", nil)
				h.DocDetail(rr, req)
			},
		},
		{
			name: "projects_list",
			path: "/projects",
			call: func(rr *httptest.ResponseRecorder) {
				req := httptest.NewRequest("GET", "/projects", nil)
				h.ProjectsList(rr, req)
			},
		},
		{
			name: "project_detail_fake_slug",
			path: "/projects/2026-04-19",
			call: func(rr *httptest.ResponseRecorder) {
				req := httptest.NewRequest("GET", "/projects/2026-04-19", nil)
				h.ProjectDetail(rr, req)
			},
		},
		{
			name: "rss",
			path: "/rss.xml",
			call: func(rr *httptest.ResponseRecorder) {
				req := httptest.NewRequest("GET", "/rss.xml", nil)
				h.RSS(rr, req)
			},
		},
		{
			name: "sitemap",
			path: "/sitemap.xml",
			call: func(rr *httptest.ResponseRecorder) {
				req := httptest.NewRequest("GET", "/sitemap.xml", nil)
				h.Sitemap(rr, req)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			c.call(rr)
			if strings.Contains(rr.Body.String(), marker) {
				body := rr.Body.String()
				snippet := body
				if len(body) > 500 {
					snippet = body[:500]
				}
				t.Errorf("%s (%s) leaked diary marker; status=%d body snippet:\n%s",
					c.name, c.path, rr.Code, snippet)
			}
		})
	}

	// 即便带预览管理员头（内部测试用的 X-Preview-Admin: 1），
	// 日记也不该泄露 — 预览头只解锁 draft 预览，不解锁日记可见性。
	t.Run("preview_admin_header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Preview-Admin", "1")
		rr := httptest.NewRecorder()
		h.Home(rr, req)
		if strings.Contains(rr.Body.String(), marker) {
			t.Errorf("even with preview header, / leaked diary marker")
		}
	})
}
