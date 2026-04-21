package public_test

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// WI-2.12 Smoke: 导航栏"作品集"入口。
// 豁免：本 WI 为纯展示、静态渲染、无外部输入，未独立写异常测试
// （窄屏响应由 WI-2.15 axe/responsive 扫描覆盖）。
func TestNav_Smoke_PortfolioLinkPresent(t *testing.T) {
	h := setupWithPortfolio(t, nil)
	routes := []struct {
		path string
		call func(rr *httptest.ResponseRecorder)
	}{
		{"/", func(rr *httptest.ResponseRecorder) { h.Home(rr, httptest.NewRequest("GET", "/", nil)) }},
		{"/docs", func(rr *httptest.ResponseRecorder) { h.DocsList(rr, httptest.NewRequest("GET", "/docs", nil)) }},
		{"/projects", func(rr *httptest.ResponseRecorder) { h.ProjectsList(rr, httptest.NewRequest("GET", "/projects", nil)) }},
		{"/portfolio", func(rr *httptest.ResponseRecorder) { h.PortfolioList(rr, httptest.NewRequest("GET", "/portfolio", nil)) }},
	}
	for _, r := range routes {
		rr := httptest.NewRecorder()
		r.call(rr)
		body := rr.Body.String()
		if !strings.Contains(body, `<a href="/portfolio">作品集</a>`) {
			t.Errorf("nav link missing on %s", r.path)
		}
		// Position check: 作品集 should come after 文档 and before 关于
		idxDocs := strings.Index(body, `<a href="/docs">文档</a>`)
		idxPortfolio := strings.Index(body, `<a href="/portfolio">作品集</a>`)
		idxAbout := strings.Index(body, `<a href="/about">关于</a>`)
		if !(idxDocs < idxPortfolio && idxPortfolio < idxAbout) {
			t.Errorf("%s: nav order wrong: docs=%d portfolio=%d about=%d",
				r.path, idxDocs, idxPortfolio, idxAbout)
		}
	}
}
