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

func setup(t *testing.T, docs map[string]string, projects map[string]string) *public.Handlers {
	t.Helper()
	dir := t.TempDir()
	for name, body := range docs {
		path := filepath.Join(dir, "content", "docs", name+".md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range projects {
		path := filepath.Join(dir, "content", "projects", name+".md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := content.New(dir, logger)
	if err := store.Reload(); err != nil {
		t.Fatal(err)
	}
	tpl, err := render.NewTemplates(assets.Templates(), render.NewMarkdown())
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	// Attach a fresh SettingsDB so tests can drive site_settings-backed
	// features (about_* overrides, tagline cache, etc.).
	st, err := storage.Open(dir)
	if err != nil && !errors.Is(err, storage.ErrCorruptDB) {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := public.NewHandlers(store, tpl, logger)
	h.SettingsDB = settings.New(st.DB)
	return h
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func doc(slug, updated, status string, featured bool, extra string) string {
	body := "---\ntitle: " + titleCase(slug) + "\nslug: " + slug + "\nupdated: " + updated + "\nstatus: " + status + "\n"
	if featured {
		body += "featured: true\n"
	}
	body += extra
	body += "---\n"
	body += "content of " + slug + "\n"
	return body
}

func proj(slug, updated, status, featured string) string {
	body := "---\nslug: " + slug + "\nrepo: penguin/" + slug + "\nupdated: " + updated + "\nstatus: " + status + "\ndisplay_name: " + slug + "\ndisplay_desc: desc " + slug + "\ncategory: backend\n"
	if featured == "true" {
		body += "featured: true\n"
	}
	body += "---\n\n" + slug + " body\n"
	return body
}

// --- Home (WI-2.10, WI-2.11) -----------------------------------------------

func TestHome_Smoke_HasBasicAnchors(t *testing.T) {
	h := setup(t,
		map[string]string{
			"a": doc("a", "2026-04-10", "published", true, ""),
			"b": doc("b", "2026-04-05", "published", false, ""),
		},
		map[string]string{
			"p1": proj("p1", "2026-04-10", "active", "true"),
			"p2": proj("p2", "2026-04-08", "active", ""),
		},
	)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.Home(w, req)
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	body := w.Body.String()
	for _, frag := range []string{"Penguin", "Recently Active", "个人总结文档", "保持联系"} {
		if !strings.Contains(body, frag) {
			t.Errorf("missing %q", frag)
		}
	}
}

func TestHome_Edge_EmptyContent(t *testing.T) {
	h := setup(t, nil, nil)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.Home(w, req)
	if w.Code != 200 {
		t.Errorf("status: %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "暂无") {
		t.Errorf("empty state placeholder missing")
	}
}

// --- Docs list (WI-2.14, WI-2.15) ------------------------------------------

func TestDocsList_Smoke_ListsPublished(t *testing.T) {
	h := setup(t, map[string]string{
		"a": doc("a", "2026-04-10", "published", false, ""),
		"b": doc("b", "2026-04-05", "draft", false, ""),
		"c": doc("c", "2026-04-02", "archived", false, ""),
	}, nil)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()
	h.DocsList(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, ">A<") {
		t.Errorf("published doc missing")
	}
	if strings.Contains(body, ">B<") {
		t.Errorf("draft should not appear")
	}
	if strings.Contains(body, ">C<") {
		t.Errorf("archived should not appear in default view")
	}
}

func TestDocsList_Smoke_TagAND(t *testing.T) {
	h := setup(t, map[string]string{
		"a": doc("a", "2026-04-10", "published", false, "tags: [t1, t2]\n"),
		"b": doc("b", "2026-04-09", "published", false, "tags: [t1]\n"),
		"c": doc("c", "2026-04-08", "published", false, "tags: [t2]\n"),
	}, nil)
	req := httptest.NewRequest("GET", "/docs?tag=t1&tag=t2", nil)
	w := httptest.NewRecorder()
	h.DocsList(w, req)
	body := w.Body.String()
	// Only entry a has both tags.
	if !strings.Contains(body, ">A<") || strings.Contains(body, ">B<") || strings.Contains(body, ">C<") {
		t.Errorf("AND filter failed: %s", body)
	}
}

func TestDocsList_Edge_InvalidPageFallbacksToOne(t *testing.T) {
	docs := map[string]string{}
	for i := 0; i < 3; i++ {
		slug := "d" + string(rune('a'+i))
		docs[slug] = doc(slug, "2026-04-10", "published", false, "")
	}
	h := setup(t, docs, nil)
	for _, q := range []string{"?page=abc", "?page=-1", "?page=999"} {
		req := httptest.NewRequest("GET", "/docs"+q, nil)
		w := httptest.NewRecorder()
		h.DocsList(w, req)
		if w.Code != 200 {
			t.Errorf("%s status %d", q, w.Code)
		}
	}
}

func TestDocsList_Edge_EmptyResults(t *testing.T) {
	h := setup(t, map[string]string{
		"a": doc("a", "2026-04-10", "published", false, "category: cat-x\n"),
	}, nil)
	req := httptest.NewRequest("GET", "/docs?category=cat-y", nil)
	w := httptest.NewRecorder()
	h.DocsList(w, req)
	if !strings.Contains(w.Body.String(), "暂无内容") {
		t.Errorf("empty placeholder missing")
	}
}

// --- Doc detail (WI-2.17, WI-2.18) -----------------------------------------

func TestDocDetail_Smoke_Published(t *testing.T) {
	h := setup(t, map[string]string{
		"hello": doc("hello", "2026-04-10", "published", false, ""),
	}, nil)
	req := httptest.NewRequest("GET", "/docs/hello", nil)
	w := httptest.NewRecorder()
	h.DocDetail(w, req)
	if w.Code != 200 {
		t.Errorf("status %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Hello") {
		t.Errorf("title missing")
	}
}

func TestDocDetail_Edge_DraftNotFoundAnonymously(t *testing.T) {
	h := setup(t, map[string]string{
		"secret": doc("secret", "2026-04-10", "draft", false, ""),
	}, nil)
	req := httptest.NewRequest("GET", "/docs/secret", nil)
	w := httptest.NewRecorder()
	h.DocDetail(w, req)
	if w.Code != 404 {
		t.Errorf("draft anon should 404, got %d", w.Code)
	}
}

func TestDocDetail_Edge_DraftVisibleWithPreviewHeader(t *testing.T) {
	h := setup(t, map[string]string{
		"secret": doc("secret", "2026-04-10", "draft", false, ""),
	}, nil)
	req := httptest.NewRequest("GET", "/docs/secret", nil)
	req.Header.Set("X-Preview-Admin", "1")
	w := httptest.NewRecorder()
	h.DocDetail(w, req)
	if w.Code != 200 {
		t.Errorf("preview header should grant access, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "草稿预览") {
		t.Errorf("draft banner missing")
	}
}

func TestDocDetail_Edge_ArchivedStillAccessible(t *testing.T) {
	h := setup(t, map[string]string{
		"old": doc("old", "2026-02-01", "archived", false, ""),
	}, nil)
	req := httptest.NewRequest("GET", "/docs/old", nil)
	w := httptest.NewRecorder()
	h.DocDetail(w, req)
	if w.Code != 200 {
		t.Errorf("archived should be accessible: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "已归档") {
		t.Errorf("archived banner missing")
	}
}

func TestDocDetail_Edge_UnknownSlug404(t *testing.T) {
	h := setup(t, nil, nil)
	req := httptest.NewRequest("GET", "/docs/nope", nil)
	w := httptest.NewRecorder()
	h.DocDetail(w, req)
	if w.Code != 404 {
		t.Errorf("unknown slug: %d", w.Code)
	}
}

func TestDocDetail_Edge_TraversalSlugRejected(t *testing.T) {
	h := setup(t, nil, nil)
	req := httptest.NewRequest("GET", "/docs/..%2Fetc%2Fpasswd", nil)
	w := httptest.NewRecorder()
	h.DocDetail(w, req)
	if w.Code != 404 {
		t.Errorf("traversal should 404: %d", w.Code)
	}
}

func TestDocDetail_Smoke_PrevNextNavigation(t *testing.T) {
	h := setup(t, map[string]string{
		"a": doc("a", "2026-04-10", "published", false, ""),
		"b": doc("b", "2026-04-05", "published", false, ""),
		"c": doc("c", "2026-04-02", "published", false, ""),
	}, nil)
	req := httptest.NewRequest("GET", "/docs/b", nil)
	w := httptest.NewRecorder()
	h.DocDetail(w, req)
	body := w.Body.String()
	if !strings.Contains(body, `href="/docs/a"`) {
		t.Errorf("prev link missing: %s", body)
	}
	if !strings.Contains(body, `href="/docs/c"`) {
		t.Errorf("next link missing")
	}
}
