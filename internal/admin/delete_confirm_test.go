package admin_test

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

// Regression：删除操作必须走 data-confirm + 外部 confirm_submit.js，而不是
// 内联 onsubmit="return confirm(...)"。内联事件处理器被 CSP `script-src 'self'`
// 静默拦截，过去的确认从未弹过，误点即软删。
//
// 这条用例钉死三件事：
//  1. 至少有一份文档时，列表里 delete form 带 data-confirm 属性
//  2. 页面引入 /static/js/confirm_submit.js
//  3. 不再有内联 onsubmit="return confirm(" 残留（否则 CSP 会静默降级）

func TestDocsList_Regression_DeleteConfirmUsesExternalJS(t *testing.T) {
	b := crudSetup(t)
	// 先造两个文档，让 delete form 在页面上出现
	for _, slug := range []string{"alpha", "beta"} {
		mdBody := "---\ntitle: " + slug + "\nslug: " + slug + "\nstatus: published\nupdated: 2026-04-19\ncreated: 2026-04-19\n---\n正文\n"
		w := b.authedPost(t, "/manage/docs/new", url.Values{"csrf": {b.CSRF}, "body": {mdBody}}, b.Docs.SaveDoc)
		if w.Code != 303 {
			t.Fatalf("seed doc %s failed: %d %s", slug, w.Code, w.Body.String())
		}
	}

	w := b.authedGet(t, "/manage/docs", b.Docs.DocsList)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	out := w.Body.String()

	// 每份文档的删除表单都应有 data-confirm（含标题插值）
	for _, slug := range []string{"alpha", "beta"} {
		expect := `data-confirm="确定删除《` + slug + `》？软删到 trash/"`
		if !strings.Contains(out, expect) {
			t.Errorf("missing data-confirm for %s\ngot: %s", slug, out)
		}
	}

	// 引入外部 JS
	if !strings.Contains(out, `src="/static/js/confirm_submit.js"`) {
		t.Error("missing confirm_submit.js script tag")
	}

	// 内联 onsubmit 不能再出现（CSP 会静默阻断它）
	if strings.Contains(out, `onsubmit="return confirm(`) {
		t.Error("inline onsubmit handler still present; CSP will silently block it")
	}
}

// Regression：项目管理的删除确认同一根因，一并修。
func TestProjectsList_Regression_DeleteConfirmUsesExternalJS(t *testing.T) {
	b := crudSetup(t)
	// 直接写一份 project MD 到磁盘再 reload，避免新建项目要访问 GitHub
	projectMD := "---\ntitle: proj1\nslug: proj1\nrepo: foo/bar\ndisplay_name: Proj1\nstatus: active\nupdated: 2026-04-19\ncreated: 2026-04-19\n---\n描述\n"
	if err := writeFile(b.DataDir+"/content/projects/proj1.md", projectMD); err != nil {
		t.Fatal(err)
	}
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}

	w := b.authedGet(t, "/manage/repos", b.Projects.ReposList)
	if w.Code != 200 {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	out := w.Body.String()

	if !strings.Contains(out, `data-confirm="确定删除项目`) {
		t.Errorf("project delete form missing data-confirm\nbody: %s", out)
	}
	if !strings.Contains(out, `src="/static/js/confirm_submit.js"`) {
		t.Error("missing confirm_submit.js script tag on repos page")
	}
	if strings.Contains(out, `onsubmit="return confirm(`) {
		t.Error("inline onsubmit still present on repos page; CSP blocks it silently")
	}
}

// 异常（边界/非法）：data-confirm 里的中文《》、引号、特殊字符应被 Go 模板
// 自动 HTML-escape，不会产生属性逃逸或 JS 注入风险。
func TestDocsList_Regression_DeleteConfirmEscapesTitle(t *testing.T) {
	b := crudSetup(t)
	// 标题含 HTML 敏感字符 + 引号，探模板转义
	mdBody := "---\ntitle: '恶意\"<script>alert(1)</script>'\nslug: xx\nstatus: published\nupdated: 2026-04-19\ncreated: 2026-04-19\n---\n正文\n"
	w := b.authedPost(t, "/manage/docs/new", url.Values{"csrf": {b.CSRF}, "body": {mdBody}}, b.Docs.SaveDoc)
	if w.Code != 303 {
		t.Fatalf("seed failed: %d %s", w.Code, w.Body.String())
	}

	w = b.authedGet(t, "/manage/docs", b.Docs.DocsList)
	out := w.Body.String()

	// 原始 <script> 决不能原样写进 data-confirm 值里
	if strings.Contains(out, `data-confirm="确定删除《恶意"<script>`) {
		t.Errorf("title not escaped inside data-confirm, XSS risk")
	}
	// 必须经过 html/template 转义，至少 <script> 要变成 &lt;script&gt;
	if strings.Contains(out, "data-confirm=") && !strings.Contains(out, "&lt;script&gt;") && !strings.Contains(out, "&#34;") && !strings.Contains(out, "&#39;") {
		t.Errorf("expected escape artefacts inside data-confirm; body:\n%s", out)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
