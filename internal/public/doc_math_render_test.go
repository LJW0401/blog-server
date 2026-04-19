package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression：文档详情页必须 embed KaTeX（CSS/JS）+ math-init，
// 否则正文里的 $a_b$ / $$x=y$$ 浏览器里只会显示字面 `$...$`，公式不渲染。
// 修复前这些 link/script 都不存在；修复后三件套必须都在页面 HTML 里。
func TestDocDetail_Regression_KatexAssetsEmbedded(t *testing.T) {
	h := setup(t, map[string]string{
		"formula": doc("formula", "2026-04-18", "published", false, "") + "\n$E=mc^2$ 和块公式\n\n$$x_i = y_j$$\n",
	}, nil)
	req := httptest.NewRequest("GET", "/docs/formula", nil)
	w := httptest.NewRecorder()
	h.DocDetail(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`href="/static/math/katex.min.css"`,
		`src="/static/math/katex.min.js"`,
		`src="/static/math/auto-render.min.js"`,
		`src="/static/math/math-init.js"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/docs/formula missing asset %q", want)
		}
	}
	// 公式源码必须原样出现在 HTML 里，交给浏览器端 KaTeX 扫描渲染；
	// goldmark 不应在服务端就把 $..$ 吃掉或转义
	if !strings.Contains(body, "$E=mc^2$") {
		t.Errorf("inline math literal missing in HTML; goldmark may have eaten it")
	}
	if !strings.Contains(body, "$$x_i = y_j$$") {
		t.Errorf("display math literal missing in HTML")
	}
}
