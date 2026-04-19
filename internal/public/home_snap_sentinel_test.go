package public_test

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/assets"
)

// Smoke：首页渲染出 .page-2-sentinel 元素（放在 page-2 和 footer 之间），
// 用来作为"从 footer 往回滑吸附到 page-2 底部"的吸附锚点。
//
// 实际滚动吸附行为需要浏览器才能验证，这里只能断言：
//  1. home.html DOM 含 sentinel
//  2. 位置在 .page-2 之后、 </main>（即 footer）之前
//  3. theme.css 声明了 .page-2-sentinel 的 scroll-snap-align: end
//  4. 仅 home 有（html.snap-y 作用域）——其他页不会渲染 sentinel
func TestHome_Smoke_Page2SentinelPresent(t *testing.T) {
	h := setup(t, nil, nil)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.Home(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	out := w.Body.String()

	if !strings.Contains(out, `<div class="page-2-sentinel"`) {
		t.Errorf("home missing .page-2-sentinel marker")
	}
	// page-2 必须在 sentinel 之前
	p2 := strings.Index(out, `class="page page-2"`)
	st := strings.Index(out, `class="page-2-sentinel"`)
	if p2 < 0 || st < 0 || p2 > st {
		t.Errorf("sentinel must come after .page-2 (p2=%d, sentinel=%d)", p2, st)
	}
	// footer 必须在 sentinel 之后
	ft := strings.Index(out, `class="footer"`)
	if ft < 0 || ft < st {
		t.Errorf("footer must come after sentinel (sentinel=%d, footer=%d)", st, ft)
	}
}

func TestHome_Smoke_SentinelCSSRule(t *testing.T) {
	css := readAssetFile(t, "css/theme.css")
	re := regexp.MustCompile(`(?s)html\.snap-y\s+\.page-2-sentinel\s*\{[^}]*scroll-snap-align:\s*end`)
	if !re.MatchString(css) {
		t.Errorf("theme.css missing .page-2-sentinel rule with scroll-snap-align: end")
	}
	// scroll-snap-stop 必须是 normal，否则向下滑会在 sentinel 被强制停住
	// 影响从 page-1 → page-2 → footer 的正常体验
	reStop := regexp.MustCompile(`(?s)html\.snap-y\s+\.page-2-sentinel\s*\{[^}]*scroll-snap-stop:\s*normal`)
	if !reStop.MatchString(css) {
		t.Errorf("theme.css .page-2-sentinel must use scroll-snap-stop: normal")
	}
}

// 异常（边界值）：其他页面（/docs）不走 snap-y，也不该有 sentinel —— 如果
// 误把 sentinel 放进 layout.html 会出现在所有页，把这条测试钉上防止误扩散
func TestHome_Edge_SentinelOnlyOnHome(t *testing.T) {
	h := setup(t, map[string]string{
		"a": newDocWithTags("a", "2026-04-18", "published", "tags: [x]\n"),
	}, nil)
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()
	h.DocsList(w, req)
	if strings.Contains(w.Body.String(), "page-2-sentinel") {
		t.Errorf("/docs should not carry sentinel element")
	}
}

// --- helpers ---

func readAssetFile(t *testing.T, path string) string {
	t.Helper()
	f, err := assets.Static().Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 0, 1024*32)
	b := make([]byte, 4096)
	for {
		n, rerr := f.Read(b)
		if n > 0 {
			buf = append(buf, b[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	return string(buf)
}
