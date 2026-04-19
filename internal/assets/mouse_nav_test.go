package assets_test

import (
	"io"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/assets"
)

// Smoke: mouse-nav.js 能通过嵌入 FS 取到，且内容实现了侧键导航约定。
func TestMouseNav_Smoke_EmbeddedAndWiresSideButtons(t *testing.T) {
	f, err := assets.Static().Open("js/mouse-nav.js")
	if err != nil {
		t.Fatalf("open js/mouse-nav.js: %v", err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)

	// 关键 API 调用必须出现
	for _, frag := range []string{
		"window.history.back",
		"window.history.forward",
		"addEventListener('mouseup'",
		"addEventListener('mousedown'",
		"e.button === 3", // back
		"e.button === 4", // forward
	} {
		if !strings.Contains(src, frag) {
			t.Errorf("mouse-nav.js missing key fragment %q", frag)
		}
	}
}

// Smoke: layout.html 里必须引用 mouse-nav.js，否则新增的文件不会被浏览器加载。
func TestLayout_Smoke_ReferencesMouseNavScript(t *testing.T) {
	f, err := assets.Templates().Open("layout.html")
	if err != nil {
		t.Fatalf("open layout.html: %v", err)
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	src := string(b)
	if !strings.Contains(src, `src="/static/js/mouse-nav.js"`) {
		t.Errorf("layout.html does not reference /static/js/mouse-nav.js")
	}
	// 必须 defer，避免在 </body> 前解析阻塞首屏
	if !strings.Contains(src, `/static/js/mouse-nav.js" defer`) {
		t.Errorf("script tag should use defer to avoid render-blocking parse")
	}
}

// Edge（非法输入 / 不应误触发）：脚本必须只对 button === 3 / 4 响应。
// 3 或 4 以外（包括 0 左键、1 中键、2 右键）不应触发 history 导航。
// 用文本扫描验证条件语义（我们没有浏览器环境可模拟真实事件）。
func TestMouseNav_Edge_IgnoresNonSideButtons(t *testing.T) {
	f, _ := assets.Static().Open("js/mouse-nav.js")
	defer f.Close()
	b, _ := io.ReadAll(f)
	src := string(b)

	// 不应出现 button === 0/1/2 → 导航的分支
	for _, bad := range []string{"button === 0", "button === 1", "button === 2"} {
		if strings.Contains(src, bad) {
			t.Errorf("script reacts to unexpected mouse button: %q", bad)
		}
	}
	// 导航调用必须被 3/4 的条件守卫着：搜 history.back 后应能在同一行或之前找到 === 3
	// 搜代码侧的调用（带括号），跳过注释里提到 history.back/forward 的描述
	idxBack := strings.Index(src, "history.back()")
	if idxBack < 0 {
		t.Fatal("history.back() call missing")
	}
	prefix := src[:idxBack]
	if !strings.Contains(prefix, "=== 3") {
		t.Errorf("history.back() not guarded by button === 3 check")
	}
	idxForward := strings.Index(src, "history.forward()")
	if idxForward < 0 {
		t.Fatal("history.forward() call missing")
	}
	prefix = src[:idxForward]
	if !strings.Contains(prefix, "=== 4") {
		t.Errorf("history.forward() not guarded by button === 4 check")
	}
}

// Edge（失败依赖 / 浏览器兼容）：脚本必须是 IIFE 或严格自封闭的，
// 不能污染全局作用域引起与其它脚本冲突。
func TestMouseNav_Edge_NoGlobalLeakage(t *testing.T) {
	f, _ := assets.Static().Open("js/mouse-nav.js")
	defer f.Close()
	b, _ := io.ReadAll(f)
	src := string(b)

	// IIFE 标志：文件应以 (function 开头（允许 'use strict' 在里面）
	trimmed := strings.TrimSpace(src)
	// 跳过注释
	for strings.HasPrefix(trimmed, "//") {
		nl := strings.IndexByte(trimmed, '\n')
		if nl < 0 {
			break
		}
		trimmed = strings.TrimSpace(trimmed[nl+1:])
	}
	if !strings.HasPrefix(trimmed, "(function") {
		t.Errorf("script should be an IIFE to avoid global leakage; starts with: %q", trimmed[:min(50, len(trimmed))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
