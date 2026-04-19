package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/github"
)

// Smoke: README 摘要作为 Markdown 渲染进一个小方格容器。
func TestProjectDetail_Smoke_ReadmeExcerptRendersMarkdownInBox(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-10", "active", ""),
	})
	h.GitHubCache = &fakeCache{entries: map[string]*github.CacheEntry{
		"penguin/a": {
			Repo:         "penguin/a",
			LastSyncedAt: time.Now(),
			Info: &github.RepoInfo{
				Stars:         1,
				Language:      "Go",
				PushedAt:      time.Now(),
				ReadmeExcerpt: "# Alpha\n\nHello **world**. See [docs](https://example.com) and `code`.",
			},
		},
	}}
	req := httptest.NewRequest("GET", "/projects/a", nil)
	w := httptest.NewRecorder()
	h.ProjectDetail(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()

	if !strings.Contains(body, `class="readme-excerpt-box"`) {
		t.Errorf("excerpt box container missing")
	}
	for _, frag := range []string{
		">Alpha</h1>", // heading rendered
		"<strong>world</strong>",
		`href="https://example.com"`,
		"<code>code</code>",
	} {
		if !strings.Contains(body, frag) {
			t.Errorf("markdown fragment %q not rendered in readme excerpt; body: %s", frag, body)
		}
	}
}

// Edge（边界值/缺失输入）：README 摘要为空 → 整个 .readme-excerpt 段不渲染。
func TestProjectDetail_Edge_ReadmeExcerptEmptyHidesSection(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-10", "active", ""),
	})
	h.GitHubCache = &fakeCache{entries: map[string]*github.CacheEntry{
		"penguin/a": {
			Repo: "penguin/a", LastSyncedAt: time.Now(),
			Info: &github.RepoInfo{Stars: 1, Language: "Go", PushedAt: time.Now(), ReadmeExcerpt: ""},
		},
	}}
	req := httptest.NewRequest("GET", "/projects/a", nil)
	w := httptest.NewRecorder()
	h.ProjectDetail(w, req)
	body := w.Body.String()

	if strings.Contains(body, "README 摘要") {
		t.Errorf("empty excerpt should not render section header")
	}
	if strings.Contains(body, "readme-excerpt-box") {
		t.Errorf("empty excerpt should not render container box")
	}
}

// Edge（非法输入 / XSS）：README 中的 <script> / <img onerror> 必须被转义，
// 不得作为活跃 HTML 注入。用的是安全版 markdown 渲染器（非 Unsafe）。
func TestProjectDetail_Edge_ReadmeExcerptEscapesInlineHTML(t *testing.T) {
	h := setup(t, nil, map[string]string{
		"a": proj("a", "2026-04-10", "active", ""),
	})
	malicious := "<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\nplain **text** ok"
	h.GitHubCache = &fakeCache{entries: map[string]*github.CacheEntry{
		"penguin/a": {
			Repo: "penguin/a", LastSyncedAt: time.Now(),
			Info: &github.RepoInfo{Stars: 1, Language: "Go", PushedAt: time.Now(), ReadmeExcerpt: malicious},
		},
	}}
	req := httptest.NewRequest("GET", "/projects/a", nil)
	w := httptest.NewRecorder()
	h.ProjectDetail(w, req)
	body := w.Body.String()

	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("raw <script> leaked through to output: %s", body)
	}
	if strings.Contains(body, "onerror=alert") {
		t.Errorf("raw onerror handler leaked through to output")
	}
	// 正常 markdown 仍被渲染
	if !strings.Contains(body, "<strong>text</strong>") {
		t.Errorf("legit markdown not rendered alongside escaped HTML")
	}
}
