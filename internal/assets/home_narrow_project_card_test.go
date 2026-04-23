package assets_test

import (
	"regexp"
	"strings"
	"testing"
)

// 窄屏模式下，首页"主要开源项目"卡片 (.project-card) 高度需和"个人总结文档"
// (.doc-item) 一致。.project-card 桌面态使用 aspect-ratio: 1 / 1.1 让卡片接近
// 方形，但单列塌缩后会变成一堵 400px+ 的墙；.doc-item 则是按内容自适应的
// 紧凑横条。锁死方案：窄屏 @media (max-width: 860px) 里 .project-card 必须
// 显式把 aspect-ratio 改回 auto（否则阻止不了塌缩后的巨型方块）。
func TestTheme_NarrowProjectCardMatchesDocItemHeight(t *testing.T) {
	css := readTheme(t)

	narrow := extractNarrowMediaBlock(t, css)

	rule := firstRuleWithSelector(narrow, ".project-card")
	if rule == "" {
		t.Fatal("窄屏 @media 未覆盖 .project-card —— 单列塌缩后会出现巨型方块，与 .doc-item 高度不一致")
	}

	if !regexp.MustCompile(`aspect-ratio\s*:\s*auto`).MatchString(rule) {
		t.Errorf(".project-card 窄屏覆盖未把 aspect-ratio 改回 auto：%q", rule)
	}
}

// extractNarrowMediaBlock 返回 @media (max-width: 860px) 块的大括号内内容。
func extractNarrowMediaBlock(t *testing.T, css string) string {
	t.Helper()
	marker := "@media (max-width: 860px)"
	i := strings.Index(css, marker)
	if i < 0 {
		t.Fatalf("未找到 %q", marker)
	}
	openRel := strings.Index(css[i:], "{")
	if openRel < 0 {
		t.Fatal("@media 块缺少 {")
	}
	start := i + openRel + 1
	depth := 1
	for j := start; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[start:j]
			}
		}
	}
	t.Fatal("@media 块未闭合")
	return ""
}

// firstRuleWithSelector 在 block 内查找选择器为 sel（单一选择器且无分组）
// 的第一条规则，返回花括号内内容。简化实现：匹配 "sel {" 或 "sel\n{"。
func firstRuleWithSelector(block, sel string) string {
	re := regexp.MustCompile(`(^|\n|\})\s*` + regexp.QuoteMeta(sel) + `\s*\{`)
	loc := re.FindStringIndex(block)
	if loc == nil {
		return ""
	}
	openIdx := strings.Index(block[loc[0]:loc[1]], "{")
	start := loc[0] + openIdx + 1
	depth := 1
	for j := start; j < len(block); j++ {
		switch block[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return block[start:j]
			}
		}
	}
	return ""
}
