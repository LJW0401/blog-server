package public_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/github"
)

// fakeCache is a CacheReader for tests.
type fakeCache struct {
	entries map[string]*github.CacheEntry
}

func (f *fakeCache) Get(_ context.Context, repo string) (*github.CacheEntry, error) {
	return f.entries[repo], nil
}

func (f *fakeCache) List(_ context.Context) ([]*github.CacheEntry, error) {
	out := make([]*github.CacheEntry, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e)
	}
	return out, nil
}

// --- Projects list ---------------------------------------------------------

func TestProjectsList_Smoke_DisplaysFixtures(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-10", "active", "true"),
		"b": proj("b", "2026-04-08", "active", ""),
	})
	h.GitHubCache = &fakeCache{entries: map[string]*github.CacheEntry{
		"penguin/a": {Repo: "penguin/a", LastSyncedAt: time.Now(), Info: &github.RepoInfo{Stars: 10, Forks: 1, Language: "Go", PushedAt: time.Now()}},
		"penguin/b": {Repo: "penguin/b", LastSyncedAt: time.Now(), Info: &github.RepoInfo{Stars: 5, Language: "Rust", PushedAt: time.Now()}},
	}}
	req := httptest.NewRequest("GET", "/projects", nil)
	w := httptest.NewRecorder()
	h.ProjectsList(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "全部项目") {
		t.Errorf("页面标题缺失")
	}
	if !strings.Contains(body, "Go") || !strings.Contains(body, "Rust") {
		t.Errorf("langs missing")
	}
}

func TestProjectsList_Smoke_FeaturedLandsOnTop(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"featured-proj": proj("featured-proj", "2026-04-10", "active", "true"),
	})
	h.GitHubCache = &fakeCache{entries: map[string]*github.CacheEntry{
		"penguin/featured-proj": {Repo: "penguin/featured-proj", LastSyncedAt: time.Now(), Info: &github.RepoInfo{Stars: 99, Language: "Go", PushedAt: time.Now()}},
	}}
	req := httptest.NewRequest("GET", "/projects", nil)
	w := httptest.NewRecorder()
	h.ProjectsList(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "Featured") {
		t.Errorf("featured kicker missing")
	}
}

func TestProjectsList_Edge_StatusFilter(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-10", "active", ""),
		"b": proj("b", "2026-04-05", "developing", ""),
	})
	req := httptest.NewRequest("GET", "/projects?status=developing", nil)
	w := httptest.NewRecorder()
	h.ProjectsList(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "/projects/b") {
		t.Errorf("developing project missing")
	}
	if strings.Contains(body, "/projects/a") {
		t.Errorf("active project should be filtered out")
	}
}

func TestProjectsList_Edge_EmptyState(t *testing.T) {
	h := setup(t, nil, nil)
	req := httptest.NewRequest("GET", "/projects", nil)
	w := httptest.NewRecorder()
	h.ProjectsList(w, req)
	if !strings.Contains(w.Body.String(), "暂无项目") {
		t.Errorf("empty placeholder missing")
	}
}

func TestProjectsList_Edge_InvalidPage(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-10", "active", ""),
	})
	for _, q := range []string{"?page=abc", "?page=-1", "?page=999"} {
		req := httptest.NewRequest("GET", "/projects"+q, nil)
		w := httptest.NewRecorder()
		h.ProjectsList(w, req)
		if w.Code != 200 {
			t.Errorf("%s status %d", q, w.Code)
		}
	}
}

func TestProjectsList_Edge_CacheMissShowsFirstSyncPlaceholder(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-10", "active", "true"),
	})
	// Empty cache — no entry for penguin/a.
	h.GitHubCache = &fakeCache{entries: map[string]*github.CacheEntry{}}
	req := httptest.NewRequest("GET", "/projects", nil)
	w := httptest.NewRecorder()
	h.ProjectsList(w, req)
	if !strings.Contains(w.Body.String(), "正在首次同步") {
		t.Errorf("first-sync placeholder missing")
	}
}

// --- Project detail --------------------------------------------------------

func TestProjectDetail_Smoke_RendersWithCache(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-10", "active", ""),
	})
	h.GitHubCache = &fakeCache{entries: map[string]*github.CacheEntry{
		"penguin/a": {Repo: "penguin/a", LastSyncedAt: time.Now(), Info: &github.RepoInfo{Stars: 42, Forks: 3, Language: "Go", PushedAt: time.Now(), ReadmeExcerpt: "Hi README"}},
	}}
	req := httptest.NewRequest("GET", "/projects/a", nil)
	w := httptest.NewRecorder()
	h.ProjectDetail(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "a body") {
		t.Errorf("MD body missing")
	}
	if !strings.Contains(body, "★ 42") {
		t.Errorf("stars missing: %s", body)
	}
	if !strings.Contains(body, "Hi README") {
		t.Errorf("README excerpt missing")
	}
}

func TestProjectDetail_Edge_Unknown(t *testing.T) {
	h := setup(t, nil, nil)
	req := httptest.NewRequest("GET", "/projects/nope", nil)
	w := httptest.NewRecorder()
	h.ProjectDetail(w, req)
	if w.Code != 404 {
		t.Errorf("status %d", w.Code)
	}
}

func TestProjectDetail_Edge_CacheMissingFirstSync(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-10", "active", ""),
	})
	h.GitHubCache = &fakeCache{entries: map[string]*github.CacheEntry{}}
	req := httptest.NewRequest("GET", "/projects/a", nil)
	w := httptest.NewRecorder()
	h.ProjectDetail(w, req)
	if !strings.Contains(w.Body.String(), "正在首次同步") {
		t.Errorf("first-sync placeholder missing")
	}
}

func TestProjectDetail_Edge_RemoteGone(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-10", "active", ""),
	})
	h.GitHubCache = &fakeCache{entries: map[string]*github.CacheEntry{
		"penguin/a": {
			Repo:         "penguin/a",
			LastSyncedAt: time.Now(),
			Info:         &github.RepoInfo{Stars: 10, Language: "Go", PushedAt: time.Now()},
			LastError:    "github: not found: /repos/penguin/a",
		},
	}}
	req := httptest.NewRequest("GET", "/projects/a", nil)
	w := httptest.NewRecorder()
	h.ProjectDetail(w, req)
	if !strings.Contains(w.Body.String(), "远端仓库不可达") {
		t.Errorf("remote-gone banner missing")
	}
}

// Regression: projects with status=active/developing must appear in the
// homepage's "主要开源项目" (pickFeatured) slot. Earlier code only accepted
// status=published, which silently filtered every project out.
func TestHome_Edge_FeaturedProjectsIncludeActiveAndDeveloping(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-10", "active", "true"),
		"b": proj("b", "2026-04-09", "developing", ""),
		"c": proj("c", "2026-04-08", "archived", ""), // must NOT appear
	})
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.Home(w, req)
	body := w.Body.String()
	// The project cards in "主要开源项目" use .Slug in href=/projects/<slug>.
	if !strings.Contains(body, `class="project-card" href="/projects/a"`) {
		t.Errorf("active featured project a missing")
	}
	if !strings.Contains(body, `class="project-card" href="/projects/b"`) {
		t.Errorf("developing project b missing")
	}
	if strings.Contains(body, `class="project-card" href="/projects/c"`) {
		t.Error("archived project c should not appear")
	}
}

// --- Home Recently Active derivation (WI-3.15, WI-3.16) -------------------

func TestHome_Smoke_RecentlyActiveDerived(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-05", "active", ""),
		"b": proj("b", "2026-04-10", "active", ""),
		"c": proj("c", "2026-04-08", "active", ""),
		"d": proj("d", "2026-04-09", "archived", ""), // should be skipped
	})
	// Cache has later pushed_at for b; others fall back to Updated.
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	h.GitHubCache = &fakeCache{entries: map[string]*github.CacheEntry{
		"penguin/b": {Repo: "penguin/b", LastSyncedAt: now, Info: &github.RepoInfo{PushedAt: now, Stars: 99, Language: "Go"}},
	}}
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.Home(w, req)
	body := w.Body.String()
	// Should see a/b/c, not d.
	if !strings.Contains(body, "/projects/b") {
		t.Errorf("b should appear")
	}
	if strings.Contains(body, "/projects/d") {
		t.Errorf("archived d should not appear")
	}
	// First recent repo card should be b (freshest via cache push_at).
	firstCard := strings.Index(body, `class="repo-card"`)
	if firstCard == -1 {
		t.Fatal("no repo-card rendered")
	}
	// Look at the first 500 chars after the first repo-card for slug b.
	window := body[firstCard:]
	if len(window) > 500 {
		window = window[:500]
	}
	if !strings.Contains(window, "/projects/b") {
		t.Errorf("most-recent repo should be b; first window = %s", window)
	}
}
