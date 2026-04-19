package assets_test

import (
	"strings"
	"testing"
)

// Regression: 暗色模式下 .about-card 必须有背景覆盖。
// 原 bug：.about-card 在亮色规则里硬编码 background: #fff，
// 而 @media (prefers-color-scheme: dark) 里只覆盖了 .repo-card / .project-card /
// .doc-item / .proj，没管 .about-card，所以暗色模式下"关于我"卡片仍是白的。
func TestTheme_Regression_AboutCardDarkModeOverride(t *testing.T) {
	css := readTheme(t)

	darkStart := strings.Index(css, "@media (prefers-color-scheme: dark)")
	if darkStart < 0 {
		t.Fatal("dark-mode @media block not found")
	}
	// Find the matching closing brace of the @media block. It's the first `}\n}`
	// sequence after the start (inner rule close + outer media close) — but a
	// simpler heuristic: scan balanced braces starting from the `{` after the
	// @media selector.
	openIdx := strings.Index(css[darkStart:], "{")
	if openIdx < 0 {
		t.Fatal("dark-mode @media opening brace not found")
	}
	depth := 0
	endRel := -1
	for i := openIdx; i < len(css)-darkStart; i++ {
		c := css[darkStart+i]
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				endRel = i
			}
		}
		if endRel >= 0 {
			break
		}
	}
	if endRel < 0 {
		t.Fatal("dark-mode @media block unbalanced")
	}
	darkBlock := css[darkStart : darkStart+endRel+1]

	if !strings.Contains(darkBlock, ".about-card") {
		t.Errorf("dark-mode block does not override .about-card — card will show white #fff in dark mode")
	}
	// Sanity: 该覆盖应设置 background（不只是 border）。
	// 在 dark 块中找到 .about-card 选择器后到 } 的片段。
	aboutIdx := strings.Index(darkBlock, ".about-card")
	if aboutIdx < 0 {
		return // already failed above
	}
	ruleEnd := strings.Index(darkBlock[aboutIdx:], "}")
	if ruleEnd < 0 {
		t.Fatal(".about-card dark rule not closed")
	}
	rule := darkBlock[aboutIdx : aboutIdx+ruleEnd]
	if !strings.Contains(rule, "background") {
		t.Errorf("dark-mode .about-card override missing background declaration: %q", rule)
	}

	// 技能栈等 pills 也是硬编码 #f0f0f2，暗色下会变成亮灰 → 必须覆盖。
	if !strings.Contains(darkBlock, ".about-card .pill") {
		t.Errorf("dark-mode block does not override .about-card .pill — tech stack pills will show light #f0f0f2 in dark mode")
	}
}
