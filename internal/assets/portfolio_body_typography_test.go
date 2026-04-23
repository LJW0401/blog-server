package assets_test

import (
	"regexp"
	"testing"
)

// Regression: 公开作品集详情页 (.portfolio-body) 的 markdown 排版必须与
// 管理后台编辑预览 (.doc-body) 对齐。
//
// 原 bug：.portfolio-body 只声明了 line-height / font-size，
// 缺少对 h1/h2/h3、p、ul/ol、pre、blockquote 的规则；
// 同一份 goldmark HTML 在后台预览里嵌套列表有缩进、段落有间距，
// 到了 /portfolio/<slug> 变成无缩进、无段距的一坨。
//
// 锁死方案：.portfolio-body 必须对常见 markdown 元素都有对应规则
// （独立规则或与 .doc-body 合并的分组选择器皆可）。
func TestTheme_Regression_PortfolioBodyTypography(t *testing.T) {
	css := readTheme(t)

	descendants := []string{"h1", "h2", "h3", "p", "ul", "ol", "pre", "blockquote"}
	for _, d := range descendants {
		re := regexp.MustCompile(`\.portfolio-body\s+` + d + `\b`)
		if !re.MatchString(css) {
			t.Errorf(".portfolio-body 缺少对 %s 的排版规则 —— 公开详情页渲染会与 .doc-body 预览不一致", d)
		}
	}
}
