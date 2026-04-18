package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Smoke: 个人总结文档 卡片现在渲染摘要。
func TestHome_Smoke_FeaturedDocExcerptRendered(t *testing.T) {
	h := setup(t, map[string]string{
		"a": "---\ntitle: A\nslug: a\nupdated: 2026-04-10\nstatus: published\nexcerpt: 一段精心写过的摘要\n---\nbody\n",
	}, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	body := rr.Body.String()
	if !strings.Contains(body, `class="doc-excerpt"`) {
		t.Errorf(".doc-excerpt element not rendered")
	}
	if !strings.Contains(body, "一段精心写过的摘要") {
		t.Errorf("excerpt text missing from rendered card")
	}
}

// Edge（空/缺失输入）：frontmatter excerpt 留空且 body 也空 →
// Entry.Excerpt 为 ""；模板 {{ if .Excerpt }} 守卫生效，不渲染空元素。
// （content 包会用 body 前 120 字自动补 excerpt，所以这里必须连 body 也留空。）
func TestHome_Edge_FeaturedDocNoExcerptNoEmptyElement(t *testing.T) {
	h := setup(t, map[string]string{
		"a": "---\ntitle: A\nslug: a\nupdated: 2026-04-10\nstatus: published\n---\n",
	}, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.Home(rr, req)
	body := rr.Body.String()
	if strings.Contains(body, `class="doc-excerpt"`) {
		t.Errorf("no excerpt → should not render .doc-excerpt container")
	}
	// But card itself still rendered.
	if !strings.Contains(body, `href="/docs/a"`) {
		t.Errorf("card itself missing")
	}
}
