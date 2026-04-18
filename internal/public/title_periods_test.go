package public_test

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Smoke：主要页面的 <h1>/<h2>/<h4> 里尾部不应再含"。"——视觉设计决定。
func TestTitles_Smoke_NoTrailingChinesePeriodInHeaders(t *testing.T) {
	h := setup(t, map[string]string{
		"a": "---\ntitle: A\nslug: a\nupdated: 2026-04-10\nstatus: published\n---\nbody\n",
	}, map[string]string{
		"p": "---\nslug: p\nrepo: o/p\ndisplay_name: P\nstatus: active\ncreated: 2026-01-01\nupdated: 2026-01-01\n---\nbody\n",
	})
	// Any <h1|h2|h3|h4> whose closing tag is preceded by a Chinese period is a
	// regression. 用显式正则避免写死具体标题。
	re := regexp.MustCompile(`。</h[1-6]>`)
	for _, c := range []struct{ path, handlerName string }{
		{"/", "Home"},
		{"/docs", "DocsList"},
		{"/projects", "ProjectsList"},
	} {
		t.Run(c.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", c.path, nil)
			switch c.handlerName {
			case "Home":
				h.Home(rr, req)
			case "DocsList":
				h.DocsList(rr, req)
			case "ProjectsList":
				h.ProjectsList(rr, req)
			}
			body := rr.Body.String()
			if m := re.FindString(body); m != "" {
				t.Errorf("%s: 标题尾仍含 Chinese period: match=%q", c.path, m)
			}
		})
	}
}

// Edge（边界 / 语义完整性）：标题文字本身必须还在——测试不能因为模板全被改乱了
// 反而误过。
func TestTitles_Edge_TitleTextStillRenders(t *testing.T) {
	h := setup(t, nil, nil)
	for _, c := range []struct {
		path, handler, want string
	}{
		{"/", "Home", "关于我</h2>"},
		{"/", "Home", "主要开源项目</h2>"},
		{"/", "Home", "个人总结文档</h2>"},
		{"/docs", "DocsList", "文档</h1>"},
		{"/projects", "ProjectsList", "项目</h1>"},
		{"/", "Home", "保持联系</h4>"}, // footer is in layout, always present
	} {
		t.Run(c.want, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", c.path, nil)
			switch c.handler {
			case "Home":
				h.Home(rr, req)
			case "DocsList":
				h.DocsList(rr, req)
			case "ProjectsList":
				h.ProjectsList(rr, req)
			}
			if !strings.Contains(rr.Body.String(), c.want) {
				t.Errorf("expected title fragment %q missing", c.want)
			}
		})
	}
}
