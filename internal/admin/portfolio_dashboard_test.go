package admin_test

import (
	"strings"
	"testing"
)

// Smoke: 作品集统计卡现在落在 /manage/portfolio 列表页顶部，Dashboard 只留导航链接。
// 豁免异常：纯聚合计数无外部输入。未登录 → middleware（WI-3.6 覆盖）；
// store 异常 → WI-1.3 覆盖；0 条边界 → 下面的 zero-entries 用例覆盖。
func TestPortfolioList_Smoke_StatsCardAndCounts(t *testing.T) {
	b := crudSetup(t)
	// Seed 1 published, 1 draft, 1 archived.
	seedPortfolioFile(t, b, "p-pub", portfolioMD("p-pub", "P Pub", false, 0))
	seedPortfolioFile(t, b, "p-draft", strings.Replace(
		portfolioMD("p-draft", "P Draft", false, 0),
		"status: published", "status: draft", 1))
	seedPortfolioFile(t, b, "p-arc", strings.Replace(
		portfolioMD("p-arc", "P Arc", false, 0),
		"status: published", "status: archived", 1))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}

	w := b.authedGet(t, "/manage/portfolio", b.Portfolio.List)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `class="admin-stats-bar"`) {
		t.Error("admin-stats-bar markup missing")
	}
	// Header 顶栏仍挂着「新建作品」链接，统计条不再重复
	if !strings.Contains(body, `href="/manage/portfolio/new"`) {
		t.Error("header '新建作品' link missing")
	}
	for _, want := range []string{
		"published <strong>1</strong>",
		"draft <strong>1</strong>",
		"archived <strong>1</strong>",
		"共 3 条",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("portfolio list missing count: %q", want)
		}
	}
}

// Smoke: 0 条时统计卡仍渲染（不裸崩、显示 "共 0 条"）。
func TestPortfolioList_Smoke_StatsCardRendersWithZeroEntries(t *testing.T) {
	b := crudSetup(t)
	w := b.authedGet(t, "/manage/portfolio", b.Portfolio.List)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `class="admin-stats-bar"`) {
		t.Error("stats bar should render even with 0 entries")
	}
	if !strings.Contains(body, "共 0 条") {
		t.Error("zero total not shown")
	}
}

// Smoke: Dashboard 不再显示作品集卡片（仅保留导航链接）。
// 豁免异常：纯模板删除，无新输入 / 分支。
func TestDashboard_Smoke_NoPortfolioCard(t *testing.T) {
	b := crudSetup(t)
	seedPortfolioFile(t, b, "p-pub", portfolioMD("p-pub", "P Pub", false, 0))
	if err := b.Content.Reload(); err != nil {
		t.Fatal(err)
	}
	b.Admin.Content = b.Content
	w := b.authedGet(t, "/manage", b.Admin.Dashboard)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	// 旧卡片标记与新的统计条都不应出现在 Dashboard
	for _, gone := range []string{
		"作品集</h3>",
		"dashboard-card-stats",
		"admin-stats-bar",
		"共 1 条",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("dashboard still shows removed card markup: %q", gone)
		}
	}
	// 导航链接仍保留
	if !strings.Contains(body, `href="/manage/portfolio"`) {
		t.Error("dashboard missing portfolio nav link")
	}
}
