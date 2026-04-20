package admin_test

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/admin"
)

// aboutBundle extends crudBundle with an AboutHandlers wired to the same
// DataDir so tests can drive the /manage/about flow end-to-end.
type aboutBundle struct {
	*crudBundle
	About *admin.AboutHandlers
}

func aboutSetup(t *testing.T) *aboutBundle {
	t.Helper()
	b := crudSetup(t)
	return &aboutBundle{
		crudBundle: b,
		About:      &admin.AboutHandlers{Parent: b.Admin, DataDir: b.DataDir},
	}
}

// Smoke：首次 GET /manage/about（文件不存在）→ 200，textarea 预填默认文案
// （让管理员从模板改而不是从空白开始），编辑/预览 tabs 都在。
func TestAbout_Smoke_FirstVisitPrefillsDefault(t *testing.T) {
	b := aboutSetup(t)
	w := b.authedGet(t, "/manage/about", b.About.AboutPage)

	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `name="body"`) {
		t.Errorf("editor textarea 缺失")
	}
	if !strings.Contains(body, "默认文案") {
		t.Errorf("首次访问 textarea 应预填默认文案，便于管理员在模板上修改")
	}
	if !strings.Contains(body, "编辑</button>") || !strings.Contains(body, "预览</button>") {
		t.Errorf("编辑/预览 tabs 缺失")
	}
	if !strings.Contains(body, `action="/manage/about"`) {
		t.Errorf("form action 不对")
	}
}

// Smoke：一旦保存过（即使保存为空字符串）后，编辑器就以文件内容为准，
// 不再注入默认文案 —— 否则管理员"清空"操作会被默认覆盖，改不掉。
func TestAbout_Smoke_SavedEmptyFileSuppressesDefault(t *testing.T) {
	b := aboutSetup(t)
	_ = b.authedPost(t, "/manage/about", url.Values{"csrf": {b.CSRF}, "body": {""}}, b.About.AboutSubmit)

	w := b.authedGet(t, "/manage/about", b.About.AboutPage)
	if strings.Contains(w.Body.String(), "默认文案") {
		t.Errorf("保存过空文件后不应再回填默认文案")
	}
}

// Smoke：POST 正文 → 写文件、重定向 303 → /manage/about?m=saved；再 GET 应回显。
func TestAbout_Smoke_SavePersistsAndRendersBack(t *testing.T) {
	b := aboutSetup(t)
	form := url.Values{"csrf": {b.CSRF}, "body": {"我是 **Penguin**"}}
	w := b.authedPost(t, "/manage/about", form, b.About.AboutSubmit)
	if w.Code != 303 {
		t.Fatalf("status %d, want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/manage/about") || !strings.Contains(loc, "m=saved") {
		t.Errorf("redirect = %q", loc)
	}
	// 文件真的落盘了
	raw, err := os.ReadFile(filepath.Join(b.DataDir, "content", "about.md"))
	if err != nil {
		t.Fatalf("about.md 未写入：%v", err)
	}
	if string(raw) != "我是 **Penguin**" {
		t.Errorf("file content = %q", raw)
	}
	// 再次 GET，编辑器里应能看见刚保存的内容
	w2 := b.authedGet(t, "/manage/about?m=saved", b.About.AboutPage)
	if !strings.Contains(w2.Body.String(), "我是 **Penguin**") {
		t.Errorf("编辑器没回显已保存内容")
	}
	if !strings.Contains(w2.Body.String(), "已保存") {
		t.Errorf("成功横幅缺失")
	}
}

// Edge（权限认证）：没 cookie → 401。
func TestAbout_Edge_SaveWithoutCookieUnauthorized(t *testing.T) {
	b := aboutSetup(t)
	form := url.Values{"csrf": {b.CSRF}, "body": {"x"}}
	req := httptest.NewRequest("POST", "/manage/about", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "test/ua")
	w := httptest.NewRecorder()
	b.About.AboutSubmit(w, req)
	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// Edge（权限认证 / CSRF）：cookie 有但 CSRF 错 → 403。
func TestAbout_Edge_SaveWithBadCSRFForbidden(t *testing.T) {
	b := aboutSetup(t)
	form := url.Values{"csrf": {"wrong-token"}, "body": {"x"}}
	w := b.authedPost(t, "/manage/about", form, b.About.AboutSubmit)
	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
	// 错误的 CSRF 必须保证没有任何副作用（文件不被写）
	if _, err := os.Stat(filepath.Join(b.DataDir, "content", "about.md")); err == nil {
		t.Errorf("CSRF 校验失败不应写文件")
	}
}

// Edge（异常恢复 / 原子性）：文件已有旧内容，保存新内容后旧内容被完全替换，不留残渣。
func TestAbout_Edge_SaveOverwritesExistingAtomically(t *testing.T) {
	b := aboutSetup(t)
	path := filepath.Join(b.DataDir, "content", "about.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("旧版本内容，包含一段很长的东西"), 0o600); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"csrf": {b.CSRF}, "body": {"新版"}}
	w := b.authedPost(t, "/manage/about", form, b.About.AboutSubmit)
	if w.Code != 303 {
		t.Fatalf("status %d", w.Code)
	}

	raw, _ := os.ReadFile(path)
	if string(raw) != "新版" {
		t.Errorf("file 未被完整替换：%q", raw)
	}
	// 临时文件应已被 rename 掉，不该留下
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Errorf("原子写留下了 .tmp 文件")
	}
}

// Edge（边界值）：保存空字符串是合法操作，等同"清空 about 页"，前台会 404。
func TestAbout_Edge_SaveEmptyBodyClearsAboutPage(t *testing.T) {
	b := aboutSetup(t)
	// 先写一个非空版本
	_ = b.authedPost(t, "/manage/about", url.Values{"csrf": {b.CSRF}, "body": {"hi"}}, b.About.AboutSubmit)
	// 再提交空，应该允许
	form := url.Values{"csrf": {b.CSRF}, "body": {""}}
	w := b.authedPost(t, "/manage/about", form, b.About.AboutSubmit)
	if w.Code != 303 {
		t.Fatalf("status %d", w.Code)
	}
	raw, _ := os.ReadFile(filepath.Join(b.DataDir, "content", "about.md"))
	if string(raw) != "" {
		t.Errorf("清空失败，file = %q", raw)
	}
}
