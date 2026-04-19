package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// 在标签视图下选中标签后，目录下方必须追加命中的文档列表。
// 修复前 /docs?view=tag&tag=foo 只渲染 tag 目录，用户看不到过滤结果。

func newDocWithTags(slug, updated, status, tagsFM string) string {
	return doc(slug, updated, status, false, tagsFM)
}

// Smoke：tag view 下选中 go 后，目录下方追加命中的 a、b；不列 c
func TestDocsList_Smoke_TagFilterBelowCatalog(t *testing.T) {
	h := setup(t, map[string]string{
		"a": newDocWithTags("a", "2026-04-18", "published", "tags: [go]\n"),
		"b": newDocWithTags("b", "2026-04-17", "published", "tags: [go, tool]\n"),
		"c": newDocWithTags("c", "2026-04-16", "published", "tags: [rust]\n"),
	}, nil)
	req := httptest.NewRequest("GET", "/docs?view=tag&tag=go", nil)
	w := httptest.NewRecorder()
	h.DocsList(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	out := w.Body.String()

	// 目录本身必须还在（tag cards），点中的 go 要带 active
	if !strings.Contains(out, `class="tag-catalog"`) && !strings.Contains(out, "tag-catalog") {
		t.Errorf("tag catalog missing")
	}
	// 命中的 a、b 都要出现
	for _, slug := range []string{"/docs/a", "/docs/b"} {
		if !strings.Contains(out, slug) {
			t.Errorf("filtered list missing %s", slug)
		}
	}
	// 未命中 rust 的 c 不该出现
	if strings.Contains(out, "/docs/c") {
		t.Errorf("c should not appear when filtering by tag=go, got: %s", out)
	}
}

// 边界：tag 视图但没选 tag → 只渲染目录，不渲染 list，也没有 "命中文档" 分隔
func TestDocsList_Edge_TagViewNoActiveTag_NoList(t *testing.T) {
	h := setup(t, map[string]string{
		"a": newDocWithTags("a", "2026-04-18", "published", "tags: [go]\n"),
	}, nil)
	req := httptest.NewRequest("GET", "/docs?view=tag", nil)
	w := httptest.NewRecorder()
	h.DocsList(w, req)
	out := w.Body.String()
	// 未选 tag 时，命中文档列表不该出现（即 tag-catalog 后面没有 doc-row 之类列表）
	if strings.Contains(out, "/docs/a") && strings.Contains(out, `class="doc-row"`) {
		t.Errorf("should not render doc-row list without active tag")
	}
	// 还是要渲染目录（有一个 tag card）
	if !strings.Contains(out, "tag-card") {
		t.Errorf("tag catalog should still render")
	}
}

// 边界：选中的 tag 没有任何匹配 → 命中文档区渲染 "暂无内容"（view_list 的 else 分支）
func TestDocsList_Edge_TagFilter_NoMatch(t *testing.T) {
	h := setup(t, map[string]string{
		"a": newDocWithTags("a", "2026-04-18", "published", "tags: [go]\n"),
	}, nil)
	// 选一个压根不存在的 tag——等价于 /docs?view=tag&tag=ghost
	// 用 buildTagItems 看 HREF 能不能由 handler 接住：只要 tag 参数能传到 handler 就行
	req := httptest.NewRequest("GET", "/docs?view=tag&tag=ghost", nil)
	w := httptest.NewRecorder()
	h.DocsList(w, req)
	out := w.Body.String()
	if !strings.Contains(out, "暂无内容") {
		t.Errorf("empty-state msg missing when filter matches nothing")
	}
	if strings.Contains(out, "/docs/a") {
		t.Errorf("doc a should not match ghost tag")
	}
}

// 非法输入（XSS）：tag 参数含 <script>，渲染必须被 html/template 转义
func TestDocsList_Edge_TagFilter_XSSEscaped(t *testing.T) {
	h := setup(t, map[string]string{
		"a": newDocWithTags("a", "2026-04-18", "published", "tags: [go]\n"),
	}, nil)
	req := httptest.NewRequest("GET", "/docs?view=tag&tag=%3Cscript%3Ealert(1)%3C/script%3E", nil)
	w := httptest.NewRecorder()
	h.DocsList(w, req)
	out := w.Body.String()
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Errorf("XSS payload rendered unescaped in tag view: %s", out)
	}
}
