package assets_test

import (
	"regexp"
	"testing"
)

// Regression: markdown 渲染出的表格必须有可见的分隔线。
//
// 原 bug：goldmark 启用了 GFM（包含 Tables extension），
// 把 markdown 表格转成标准 `<table><thead><th>...<tbody><td>...`，
// 但 theme.css 里根本没有 `.doc-body table`/`th`/`td` 或 `.portfolio-body`
// 对应规则，浏览器默认样式不画边框，读者看到的就是一坨对齐的纯文字，
// 完全不像表格。
//
// 锁死：对两种 markdown 容器都必须有 table + th + td 规则，且 th/td
// 必须声明 border（不接受单纯的 padding，那不画分隔线）。
func TestTheme_Regression_MarkdownTableBorders(t *testing.T) {
	css := readTheme(t)

	for _, container := range []string{".doc-body", ".portfolio-body"} {
		// table 规则必须存在（可容纳分组选择器）
		tableRe := regexp.MustCompile(regexp.QuoteMeta(container) + `\s+table\b`)
		if !tableRe.MatchString(css) {
			t.Errorf("%s 缺少 table 规则 —— 表格无排版", container)
		}

		// th/td 必须存在
		cellRe := regexp.MustCompile(regexp.QuoteMeta(container) + `\s+(th|td)\b`)
		if !cellRe.MatchString(css) {
			t.Errorf("%s 缺少 th/td 规则 —— 表格无分隔线", container)
			continue
		}

		// 且至少一条关联规则声明了 border（说明真的画了分隔线）
		blockRe := regexp.MustCompile(
			`(?s)([^{}\n]*` + regexp.QuoteMeta(container) + `\s+(?:table|th|td)\b[^{}]*)\{([^{}]*)\}`)
		sawBorder := false
		for _, m := range blockRe.FindAllStringSubmatch(css, -1) {
			body := m[2]
			if regexp.MustCompile(`\bborder(-bottom|-top|-left|-right)?\s*:`).MatchString(body) {
				sawBorder = true
				break
			}
		}
		if !sawBorder {
			t.Errorf("%s 的 table/th/td 规则均未声明 border —— 渲染出的表格仍不会出现分隔线", container)
		}
	}
}
