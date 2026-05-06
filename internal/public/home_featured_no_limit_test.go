// 主页"开源项目 / 文档"列表的回归用例：取消数量上限后，每一条 featured
// 都应渲染；非 featured 一律不出现。覆盖 quick-feature: 主页展示由 admin
// 端的 featured 开关控制，不再被旧 pickFeatured 的 4/3 限制截断。
package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Smoke: 6 个 featured 文档全部在主页出现（旧实现只会展示前 4 条）。
func TestHome_Smoke_FeaturedDocsNoLimit(t *testing.T) {
	docs := map[string]string{}
	for _, slug := range []string{"d1", "d2", "d3", "d4", "d5", "d6"} {
		docs[slug] = doc(slug, "2026-04-10", "published", true, "")
	}
	h := setup(t, docs, nil)
	rr := httptest.NewRecorder()
	h.Home(rr, httptest.NewRequest("GET", "/", nil))
	body := rr.Body.String()
	for _, slug := range []string{"d1", "d2", "d3", "d4", "d5", "d6"} {
		if !strings.Contains(body, `href="/docs/`+slug+`"`) {
			t.Errorf("doc %s missing on home", slug)
		}
	}
}

// Smoke: 5 个 featured 项目全部在主页出现（旧实现限 3 条）。
func TestHome_Smoke_FeaturedProjectsNoLimit(t *testing.T) {
	projs := map[string]string{}
	for _, slug := range []string{"p1", "p2", "p3", "p4", "p5"} {
		projs[slug] = proj(slug, "2026-04-10", "active", "true")
	}
	h := setup(t, nil, projs)
	rr := httptest.NewRecorder()
	h.Home(rr, httptest.NewRequest("GET", "/", nil))
	body := rr.Body.String()
	for _, slug := range []string{"p1", "p2", "p3", "p4", "p5"} {
		if !strings.Contains(body, `class="project-card" href="/projects/`+slug+`"`) {
			t.Errorf("project %s missing on home", slug)
		}
	}
}

// Edge / 非法输入：非 featured 的 published 文档不应在主页出现，但仍应
// 在 /docs 列表里——这里只断言主页不展示，避免把语义和列表页绑死。
func TestHome_Edge_NonFeaturedDocsHidden(t *testing.T) {
	h := setup(t, map[string]string{
		"on":  doc("on", "2026-04-10", "published", true, ""),
		"off": doc("off", "2026-04-10", "published", false, ""),
	}, nil)
	rr := httptest.NewRecorder()
	h.Home(rr, httptest.NewRequest("GET", "/", nil))
	body := rr.Body.String()
	if !strings.Contains(body, `href="/docs/on"`) {
		t.Error("featured doc must appear on home")
	}
	if strings.Contains(body, `href="/docs/off"`) {
		t.Error("non-featured doc must not appear on home")
	}
}

// Edge: 全部条目都未勾选 featured 时，主页展示空占位文案而不是渲染列表项。
func TestHome_Edge_NoFeaturedShowsPlaceholders(t *testing.T) {
	h := setup(t, map[string]string{
		"a": doc("a", "2026-04-10", "published", false, ""),
	}, map[string]string{
		"p": proj("p", "2026-04-10", "active", ""),
	})
	rr := httptest.NewRecorder()
	h.Home(rr, httptest.NewRequest("GET", "/", nil))
	body := rr.Body.String()
	if !strings.Contains(body, "暂无文档") {
		t.Error("expected 暂无文档 placeholder")
	}
	if !strings.Contains(body, "暂无项目") {
		t.Error("expected 暂无项目 placeholder")
	}
}
