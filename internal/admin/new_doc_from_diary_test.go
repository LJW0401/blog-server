package admin_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Smoke：NewDoc 收到 ?diary_date=YYYY-MM-DD 时，预填 body 包含日记正文。
func TestNewDoc_Smoke_PrefillsBodyFromDiary(t *testing.T) {
	b := crudSetup(t)

	// 写一条日记，带 frontmatter（和 diary.Store.Put 产出格式一致）
	diaryDir := filepath.Join(b.DataDir, "content", "diary")
	_ = os.MkdirAll(diaryDir, 0o700)
	body := "第一段思考\n\n第二段加粗 **word**"
	content := "---\ndate: 2026-04-19\nupdated_at: 2026-04-19T00:00:00Z\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(diaryDir, "2026-04-19.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/manage/docs/new?diary_date=2026-04-19", nil)
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(b.Cookie)
	rr := httptest.NewRecorder()
	b.Docs.NewDoc(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status %d; body: %s", rr.Code, rr.Body.String())
	}
	out := rr.Body.String()
	for _, want := range []string{
		"第一段思考",
		"第二段加粗 **word**",
		"status: draft", // 新文档的 frontmatter 模板还是在
	} {
		if !strings.Contains(out, want) {
			t.Errorf("new doc page missing fragment %q", want)
		}
	}
	if !strings.Contains(out, "title: ") {
		t.Errorf("prefilled body should still include default frontmatter title slot")
	}
}

// Edge（非法输入）：非法 diary_date 仍渲染默认空白 frontmatter，不崩、不回显输入。
func TestNewDoc_Edge_InvalidDiaryDateFallbackToDefault(t *testing.T) {
	b := crudSetup(t)

	for _, d := range []string{"../etc/passwd", "abc", "2026-13-01", "2025-02-29", ""} {
		req := httptest.NewRequest("GET", "/manage/docs/new?diary_date="+d, nil)
		req.Header.Set("User-Agent", "test/ua")
		req.AddCookie(b.Cookie)
		rr := httptest.NewRecorder()
		b.Docs.NewDoc(rr, req)
		if rr.Code != 200 {
			t.Errorf("diary_date=%q status %d; want 200", d, rr.Code)
			continue
		}
		out := rr.Body.String()
		// 不应回显任何路径相关片段
		for _, bad := range []string{"passwd", "/etc/"} {
			if strings.Contains(out, bad) {
				t.Errorf("diary_date=%q response leaked suspicious fragment %q", d, bad)
			}
		}
		// 默认 frontmatter 模板仍存在
		if !strings.Contains(out, "status: draft") {
			t.Errorf("diary_date=%q missing default frontmatter", d)
		}
	}
}

// Edge（边界值）：diary_date 合法但对应文件不存在 → 回落到默认空白 frontmatter。
func TestNewDoc_Edge_MissingDiaryFileFallsBackToDefault(t *testing.T) {
	b := crudSetup(t)
	req := httptest.NewRequest("GET", "/manage/docs/new?diary_date=2026-04-19", nil)
	req.Header.Set("User-Agent", "test/ua")
	req.AddCookie(b.Cookie)
	rr := httptest.NewRecorder()
	b.Docs.NewDoc(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "status: draft") {
		t.Errorf("missing diary file should fall back to default frontmatter")
	}
}

// Edge（权限）：未登录访问会被 authGate 挡住；NewDoc 本身不做认证，
// 这一层由路由中间件保证。此处跳过 authGate 直接调 NewDoc 只验证渲染
// 不因 diary_date 异常崩溃 — 认证拒绝路径已在 login 测试里覆盖。
// （说明性注释，不落测试用例，避免重复覆盖。）
