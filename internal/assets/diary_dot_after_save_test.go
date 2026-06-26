// 回归测试：保存日记后，月视图当天格子的小绿点应即时更新，无需整页刷新。
//
// Bug：saveDay() 成功分支只改状态文案，从不动日历 DOM，小绿点只在服务端
// SSR 时按 .HasEntry 渲染。于是新写一天的日记保存后，要等刷新才出现绿点。
// 修复要求 saveDay() 成功后即时在当前格子补/去绿点，且与服务端"空内容等同
// 删除"（store.go Put 的 TrimSpace=="" → Delete）保持一致。
//
// 无 JS 运行时，沿用本包既有的"读取嵌入资源做字符串断言"约定（见
// banner_autodismiss_test.go）来挡住回归。
package assets_test

import (
	"io"
	"strings"
	"testing"

	"github.com/penguin/blog-server/internal/assets"
)

func readDiaryJS(t *testing.T) string {
	t.Helper()
	f, err := assets.Static().Open("js/diary.js")
	if err != nil {
		t.Fatalf("open js/diary.js: %v", err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// sliceFunc 抽出从 anchor 起到下一个顶层 `}` 收尾的近似函数体，
// 仅用于把断言限定在 saveDay 的成功分支附近，避免误命中别的函数。
func sliceFunc(t *testing.T, src, anchor string) string {
	t.Helper()
	i := strings.Index(src, anchor)
	if i < 0 {
		t.Fatalf("anchor %q not found in diary.js", anchor)
	}
	rest := src[i:]
	// 取该函数到下一处 "\n  }" （两空格缩进的函数收尾）为止，足够覆盖整个 saveDay。
	end := strings.Index(rest, "\n  }")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

// Smoke：diary.js 必须包含同步小绿点的逻辑（增删 .diary-dot），
// 这是修复前完全缺失的（diary.js 此前从不出现 "diary-dot"）。
func TestDiaryJS_Smoke_HasDotSyncLogic(t *testing.T) {
	js := readDiaryJS(t)
	if !strings.Contains(js, "diary-dot") {
		t.Fatal("diary.js 未引用 .diary-dot——保存后无法即时更新月视图绿点")
	}
	// 必须既能补点也能去点，否则空内容保存（等同删除）后绿点不会消失。
	if !strings.Contains(js, "appendChild") {
		t.Error("diary.js 缺少补点逻辑（appendChild）")
	}
	if !strings.Contains(js, ".remove()") {
		t.Error("diary.js 缺少去点逻辑（.remove()）")
	}
}

// 行为绑定：saveDay() 成功分支必须调用绿点同步（syncDot），
// 否则保存成功后日历不会更新。
func TestDiaryJS_Smoke_SaveSyncsDot(t *testing.T) {
	js := readDiaryJS(t)
	save := sliceFunc(t, js, "async function saveDay()")
	if !strings.Contains(save, "syncDot(") {
		t.Errorf("saveDay() 成功后未调用 syncDot()——绿点不会即时出现:\n%s", save)
	}
}

// 一致性：syncDot 必须按"内容是否非空"决定补点/去点，
// 对齐服务端 store.go 的 TrimSpace=="" 等同删除。
func TestDiaryJS_Edge_EmptyContentRemovesDot(t *testing.T) {
	js := readDiaryJS(t)
	save := sliceFunc(t, js, "async function saveDay()")
	// 补/去点的判定必须基于 trim() 后内容是否非空，对齐服务端空内容删除。
	if !strings.Contains(save, "syncDot(") || !strings.Contains(save, "trim()") {
		t.Errorf("saveDay 未基于 trim() 后内容决定补/去绿点，可能与服务端空内容删除不一致:\n%s", save)
	}
}
